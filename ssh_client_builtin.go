package main

import (
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func localSSHAgent(addr string) (net.Conn, error) {
	const unixNet = "unix"
	return net.DialUnix(unixNet, nil, &net.UnixAddr{
		Name: addr,
		Net:  unixNet,
	})
}

func connectSSH(userName string, sshKey ssh.Signer, sshAgent agent.Agent, addr string) (*ssh.Client, error) {
	const tcpNet = "tcp"
	const implicitPort = "22"
	config := &ssh.ClientConfig{
		User: userName,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(func() (signers []ssh.Signer, err error) {
				if sshKey != nil {
					signers = append(signers, sshKey)
				}
				if sshAgent != nil {
					var agentSigners []ssh.Signer
					agentSigners, err = sshAgent.Signers()
					if err == nil {
						signers = append(signers, agentSigners...)
					}
				}
				return
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	_, _, err := net.SplitHostPort(addr)
	switch err.(type) {
	case *net.AddrError:
		addr = net.JoinHostPort(addr, implicitPort)
	}
	client, err := ssh.Dial(tcpNet, addr, config)
	if err != nil {
		return nil, fmt.Errorf("dial %q: %v", addr, err)
	}
	return client, nil
}

func connectToRemoteUnixDomainSocket(localConn net.Conn, client *ssh.Client, addr string) error {
	socketConn, err := remoteUnixDomainSocketForwardSSH(client, addr)
	if err != nil {
		return fmt.Errorf("forward remote socket %q over ssh: %v", addr, err)
	}
	defer socketConn.Close()
	connPipe(localConn, socketConn)
	return nil
}

func remoteUnixDomainSocketForwardSSH(client *ssh.Client, raddr string) (net.Conn, error) {
	// See https://github.com/openssh/openssh-portable/blob/master/PROTOCOL
	msg := struct {
		Path      string
		Reserved1 string
		Reserved2 uint32
	}{Path: raddr}
	channelType := "direct-streamlocal@openssh.com"
	channel, requests, err := client.OpenChannel(channelType, ssh.Marshal(&msg))
	if err != nil {
		return nil, fmt.Errorf("open %q channel: %v", channelType, err)
	}
	go ssh.DiscardRequests(requests)
	conn := &unixDomainSocketChannelConn{Channel: channel}
	return conn, nil
}

type unixDomainSocketChannelConn struct {
	ssh.Channel
	laddr, raddr net.TCPAddr
}

// LocalAddr is net.Conn.LocalAddr
func (t *unixDomainSocketChannelConn) LocalAddr() net.Addr {
	return &t.laddr
}

// RemoteAddr is net.Conn.RemoteAddr
func (t *unixDomainSocketChannelConn) RemoteAddr() net.Addr {
	return &t.raddr
}

// SetDeadline is net.Conn.SetDeadline
func (t *unixDomainSocketChannelConn) SetDeadline(deadline time.Time) error {
	if err := t.SetReadDeadline(deadline); err != nil {
		return err
	}
	return t.SetWriteDeadline(deadline)
}

// SetReadDeadline is net.Conn.SetReadDeadline
func (t *unixDomainSocketChannelConn) SetReadDeadline(deadline time.Time) error {
	return errors.New("ssh: unixDomainSocketChannelConn: deadline not supported")
}

// SetWriteDeadline is net.Conn.SetWriteDeadline
func (t *unixDomainSocketChannelConn) SetWriteDeadline(deadline time.Time) error {
	return errors.New("ssh: unixDomainSocketChannelConn: deadline not supported")
}
