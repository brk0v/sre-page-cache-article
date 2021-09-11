package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	PAGEMAP_LENGTH = int64(8)
	PFN_MASK       = 0x7FFFFFFFFFFFFF
	PAGEMAP_BATCH  = 64 << 10
)

var (
	pageSize = int64(syscall.Getpagesize())
	mode     = os.FileMode(0600)
)

func main() {
	pid := os.Args[1]

	pagemap, err := os.OpenFile(fmt.Sprintf("/proc/%s/pagemap", pid), os.O_RDONLY, mode)
	if err != nil {
		log.Fatal(err)
	}

	kpagecount, err := os.OpenFile("/proc/kpagecount", os.O_RDONLY, mode)
	if err != nil {
		log.Fatal(err)
	}

	maps, err := os.Open(fmt.Sprintf("/proc/%s/maps", pid))
	if err != nil {
		log.Fatal(err)
	}

	pages := make(map[int64]int64)

	scanner := bufio.NewScanner(maps)
	for scanner.Scan() {
		line := scanner.Text()
		info := strings.Fields(line)
		if len(info) == 6 && info[5] == "[vsyscall]" {
			continue
		}
		s := strings.Split(info[0], "-")
		startHex, endHex := s[0], s[1]
		start, err := strconv.ParseInt(startHex, 16, 64)
		if err != nil {
			log.Fatal(err)
		}
		end, err := strconv.ParseInt(endHex, 16, 64)
		if err != nil {
			log.Fatal(err)
		}
		pages[start/pageSize] = end / pageSize
	}

	uss := 0
	already := make(map[int64]struct{}) // don't count same addresses twice

	for start, end := range pages {
		for i := start; i <= end; i++ {
			offset := i * PAGEMAP_LENGTH
			if _, ok := already[offset]; ok {
				continue
			}

			buf := make([]byte, int(PAGEMAP_LENGTH))
			_, err := pagemap.ReadAt(buf, offset)
			if err != nil {
				log.Fatal(err)
			}

			pfn := binary.LittleEndian.Uint64(buf) & PFN_MASK
			if pfn == 0 {
				continue
			}

			_, err = kpagecount.ReadAt(buf, int64(pfn)*PAGEMAP_LENGTH)
			if err != nil {
				log.Fatal(err)
			}
			count := binary.LittleEndian.Uint64(buf)
			if count == 1 {
				uss++
			}
			already[offset] = struct{}{}
		}
	}
	fmt.Println(uss)
}
