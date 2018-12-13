package main

import (
	"io"
	"net"
	"sync"
)

func connPipe(local, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(local, remote)
		local.Close()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(remote, local)
		remote.Close()
	}()
	wg.Wait()
}
