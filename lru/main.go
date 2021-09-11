package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"
)

const (
	PAGEMAP_LENGTH = int64(8)
	PFN_MASK       = 0x7FFFFFFFFFFFFF
	PAGEMAP_BATCH  = 64 << 10

	//https://elixir.bootlin.com/linux/latest/source/include/uapi/linux/kernel-page-flags.h
	KPF_LRU    = 1 << 5
	KPF_ACTIVE = 1 << 6
)

var (
	pageSize = int64(syscall.Getpagesize())
	mode     = os.FileMode(0600)
)

func main() {
	pagemap, err := os.OpenFile("/proc/self/pagemap", os.O_RDONLY, mode)
	if err != nil {
		log.Fatal(err)
	}

	kpageflags, err := os.OpenFile("/proc/kpageflags", os.O_RDONLY, mode)
	if err != nil {
		log.Fatal(err)
	}

	path := "/var/tmp/file1.db"

	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NOATIME, mode)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	stat, err := os.Lstat(path)
	if err != nil {
		log.Fatal(err)
	}
	size := stat.Size()
	pages := size / pageSize

	// Not 100% correct code because the size could be bigger than int, don't use for production code
	mm, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	defer syscall.Munmap(mm)

	// Turn off the read ahead logic because we don't need to read more data in Page Cache
	err = syscall.Madvise(mm, syscall.MADV_RANDOM)
	if err != nil {
		log.Fatal(err)
	}

	mmPtr := uintptr(unsafe.Pointer(&mm[0]))
	cached := make([]byte, pages)

	sizePtr := uintptr(size)
	cachedPtr := uintptr(unsafe.Pointer(&cached[0]))

	ret, _, err := syscall.Syscall(syscall.SYS_MINCORE, mmPtr, sizePtr, cachedPtr)
	if ret != 0 {
		log.Fatal("syscall SYS_MINCORE failed: %v", err)
	}

	for i, p := range cached {
		// If page in Page Cache we need to populate the page table off the current
		// process wit entries. We are doing thi with a hack to bypass golang optimizations.
		if p%2 == 1 {
			_ = *(*int)(unsafe.Pointer(mmPtr + uintptr(pageSize*int64(i))))
		}
	}

	// this is a hach wich we need to notify kernel to not update reference bits
	err = syscall.Madvise(mm, syscall.MADV_SEQUENTIAL)
	if err != nil {
		log.Fatal(err)
	}

	active := int64(0)
	inactive := int64(0)
	for i := int64(0); i < pages; i++ {
		offset := ((int64(mmPtr) + i*pageSize) / pageSize) * PAGEMAP_LENGTH
		buf := make([]byte, int(PAGEMAP_LENGTH))

		_, err := pagemap.ReadAt(buf, offset)
		if err != nil {
			log.Fatal(err)
		}

		pfn := binary.LittleEndian.Uint64(buf) & PFN_MASK
		if pfn == 0 {
			continue
		}

		_, err = kpageflags.ReadAt(buf, int64(pfn)*PAGEMAP_LENGTH)
		if err != nil {
			log.Fatal(err)
		}
		flags := binary.LittleEndian.Uint64(buf)

		if flags&KPF_ACTIVE != 0 {
			active += 1
			continue
		}

		if flags&KPF_LRU != 0 {
			inactive += 1
		}
	}
	fmt.Printf("             Size\t\t\tPages\n")
	fmt.Printf("File:        %d\t\t\t%d\n", size, pages)
	fmt.Printf("Active:      %d\t\t\t%d\n", active*pageSize, active)
	fmt.Printf("Inactive:    %d\t\t\t%d\n", inactive*pageSize, inactive)
	fmt.Printf("Not charged: %d\t\t\t%d\n", size-inactive*pageSize+active*pageSize, pages-active-inactive)
}
