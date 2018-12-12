package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"text/template"
	"time"

	"github.com/google/shlex"
)

type sshClientTemplateData struct {
	LocalPort        string
	RemoteHost       string
	RemoteSocketAddr string
}

var (
	sshClientTemplate            = template.New("ssh-external-client")
	sshClientTemplateOpenSSHText = `ssh -nNT -L "{{.LocalPort}}:{{.RemoteSocketAddr}}" "{{.RemoteHost}}"`
	sshClientTemplatePuTTYText   = `putty -ssh "{{.RemoteHost}}" -L "{{.LocalPort}}:{{.RemoteSocketAddr}}"`
)

func guessFreePort() (string, int, error) {
	const tcpNet = "tcp"
	listener, err := net.ListenTCP(tcpNet, &net.TCPAddr{})
	if err != nil {
		return "", 0, fmt.Errorf("open temporary listener: %v", err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	if err := listener.Close(); err != nil {
		return "", 0, fmt.Errorf("close temporary listener: %v", err)
	}
	portInt64, err := strconv.ParseInt(port, 10, 64)
	if err != nil {
		return port, 0, err
	}
	return port, int(portInt64), nil
}

func connectToRemoteUnixDomainSocketExternalClient(localConn net.Conn, template *template.Template) error {
	var command string
	localPort, localPortInt, err := guessFreePort()
	if err != nil {
		return err
	}
	var commandBuf bytes.Buffer
	err = template.Execute(&commandBuf, sshClientTemplateData{
		LocalPort:        localPort,
		RemoteHost:       flags.SSHAddr,
		RemoteSocketAddr: flags.RemoteSocketAddr,
	})
	if err != nil {
		return fmt.Errorf("command template %q: %v", template.Tree.Root.String(), err)
	}
	command = commandBuf.String()

	var name string
	var args []string
	tokens, err := shlex.Split(command)
	if err != nil {
		return fmt.Errorf("tokenize command %q: %v", command, err)
	}
	if len(tokens) == 0 {
		return fmt.Errorf("empty command: %v", command)
	}
	name = tokens[0]
	if len(tokens) > 1 {
		args = tokens[1:]
	}

	if flags.Verbose {
		log.Printf("exec: %v", command)
	}
	cmd := exec.Command(name, args...)
	if flags.Verbose {
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Start(); err != nil {
		log.Fatalf("run external ssh client: %v", err)
	}
	defer func() {
		if cmd.ProcessState == nil || cmd.ProcessState.Exited() {
			return
		}
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("kill external ssh client process: %v", err)
		}
	}()

	commandExited := make(chan struct{})
	go func() {
		cmd.Wait()
		commandExited <- struct{}{}
	}()

	const pollInterval = time.Millisecond * 10
	socketConn, err := func() (net.Conn, error) {
		for {
			select {
			case <-commandExited:
				return nil, fmt.Errorf("ssh client process exited")
			case <-time.After(pollInterval):
				const tcpNet = "tcp"
				socketConn, err := net.DialTCP(tcpNet, nil, &net.TCPAddr{Port: localPortInt})
				if err == nil {
					return socketConn, nil
				}
			}
		}
	}()
	if err != nil {
		return err
	}

	connPipe(localConn, socketConn)
	return nil
}
