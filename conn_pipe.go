package main

import (
	"io"
	"log"
	"net"
	"sync"
)

func connPipe(local, remote net.Conn) {
	var wg sync.WaitGroup
	var shutdown bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(local, remote); err != nil {
			if shutdown {
				return
			}
			log.Printf("copy remote->local: %v", err)
		}
		shutdown = true
		local.Close()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(remote, local); err != nil {
			if shutdown {
				return
			}
			log.Printf("copy local->remote: %v", err)
		}
		shutdown = true
		remote.Close()
	}()
	wg.Wait()
}
