package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var flags struct {
	SSHUserName     string
	SSHKeyPath      string
	SSHKeyPass      string
	SSHAddr         string
	RemoteSockAddr  string
	SSHAuthSock     string
	LocalListenIP   string
	LocalListenPort int
	EnvVarName      string
	CommandName     string
	CommandArgs     []string
	Verbose         bool
}

var state struct {
	sshKey     ssh.Signer
	sshAgent   agent.Agent
	listenAddr *net.TCPAddr
}

const appName = "with-ssh-docker-socket"

var version = "SNAPSHOT"
var nonzeroExit bool

func init() {
	flags.SSHAuthSock = os.Getenv("SSH_AUTH_SOCK")
	flags.SSHUserName = os.Getenv("USER")
	flags.RemoteSockAddr = "/var/run/docker.sock"
	flags.LocalListenIP = "127.0.0.1"
	flags.EnvVarName = "DOCKER_HOST"
	flags.LocalListenPort = 0

	log.SetOutput(os.Stderr)
	log.SetPrefix(fmt.Sprintf("[%s] ", appName))
	log.SetFlags(0)
	flag.StringVar(&flags.SSHAuthSock, "ssh-auth-sock", flags.SSHAuthSock, "ssh-agent socket address ($SSH_AUTH_SOCK)")
	flag.StringVar(&flags.SSHKeyPath, "ssh-key-file", flags.SSHKeyPath, "path of an ssh key file")
	flag.StringVar(&flags.SSHKeyPath, "i", flags.SSHKeyPath, "(alias for -ssh-key-file)")
	flag.StringVar(&flags.SSHKeyPass, "ssh-key-pass", flags.SSHKeyPass, "passphrase for the ssh key file given via `-i`")
	flag.StringVar(&flags.SSHUserName, "ssh-user", "", "ssh user name")
	flag.StringVar(&flags.SSHUserName, "u", "", "(alias for -ssh-user)")
	flag.StringVar(&flags.RemoteSockAddr, "remote-socket-path", flags.RemoteSockAddr, "remote socket path")
	flag.StringVar(&flags.RemoteSockAddr, "s", flags.RemoteSockAddr, "(alias for -remote-socket-path)")
	flag.StringVar(&flags.LocalListenIP, "listen-ip", flags.LocalListenIP, "local IP to listen on")
	flag.IntVar(&flags.LocalListenPort, "listen-port", flags.LocalListenPort, "local TCP port to listen on (set to 0 to assign a random free port)")
	flag.IntVar(&flags.LocalListenPort, "p", flags.LocalListenPort, "(alias for -listen-port)")
	flag.StringVar(&flags.SSHAddr, "ssh-server-addr", flags.SSHAddr, "(remote) ssh server address")
	flag.StringVar(&flags.SSHAddr, "a", flags.SSHAddr, "(alias for -ssh-server-addr)")
	flag.StringVar(&flags.EnvVarName, "env-var-name", flags.EnvVarName, "environment variable to set")
	flag.StringVar(&flags.EnvVarName, "e", flags.EnvVarName, "(alias for -env-var-name)")
	flag.BoolVar(&flags.Verbose, "verbose", flags.Verbose, "print more logs")
	flag.BoolVar(&flags.Verbose, "v", flags.Verbose, "(alias for -verbose)")
	flag.Parse()

	if flags.SSHAddr == "" {
		flag.Usage()
		log.Fatal("error: no ssh server address specified (-ssh-server-addr / -a)")
	}

	if i := strings.IndexRune(flags.SSHAddr, '@'); i >= 0 {
		flags.SSHUserName, flags.SSHAddr = flags.SSHAddr[:i], flags.SSHAddr[i+1:]
	}

	if flag.NArg() > 0 {
		flags.CommandName = flag.Arg(0)
	}
	if flag.NArg() > 1 {
		flags.CommandArgs = flag.Args()[1:]
	}

	var sshKey ssh.Signer
	if flags.SSHKeyPath != "" {
		keyBytes, err := ioutil.ReadFile(flags.SSHKeyPath)
		if err != nil {
			log.Fatalf("read key file %q: %v", flags.SSHKeyPath, err)
		}
		sshKey, err = func() (ssh.Signer, error) {
			if flags.SSHKeyPass != "" {
				sshKey, err := ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(flags.SSHKeyPass))
				if err != nil {
					return nil, fmt.Errorf("parse+decrypt key file %q: %v", flags.SSHKeyPath, err)
				}
				return sshKey, nil
			}
			sshKey, err := ssh.ParsePrivateKey(keyBytes)
			if err != nil {
				return nil, fmt.Errorf("parse key file %q: %v", flags.SSHKeyPath, err)
			}
			return sshKey, nil
		}()
	}

	var sshAgent agent.Agent
	if sshKey == nil && flags.SSHAuthSock != "" {
		conn, err := localSSHAgent(flags.SSHAuthSock)
		if err != nil {
			log.Fatalf("connect to ssh-agent %q: %v", flags.SSHAuthSock, err)
		}
		sshAgent = agent.NewClient(conn)
	}

	listenAddr := &net.TCPAddr{
		IP:   net.ParseIP(flags.LocalListenIP),
		Port: flags.LocalListenPort,
	}

	state.sshAgent = sshAgent
	state.sshKey = sshKey
	state.listenAddr = listenAddr
}

func main() {
	if flags.Verbose {
		log.Printf("connecting to %v@%v", flags.SSHUserName, flags.SSHAddr)
	}
	client, err := connectSSH(flags.SSHUserName, state.sshKey, state.sshAgent, flags.SSHAddr)
	if err != nil {
		log.Fatal(err)
	}

	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("open ssh session: %v", err)
	}
	defer session.Close()

	const tcpNet = "tcp"
	listener, err := net.ListenTCP(tcpNet, state.listenAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	shutdown := false
	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-signals
		log.Printf("received %v signal, shutting down", s)
		shutdown = true
		listener.Close()
	}()

	if flags.Verbose {
		log.Printf("forwarding %v to socket %q on %v", listener.Addr(), flags.RemoteSockAddr, flags.SSHAddr)
	}
	envPair := fmt.Sprintf("%v=tcp://%v", flags.EnvVarName, listener.Addr())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var wg sync.WaitGroup
		for {
			localConn, err := listener.Accept()
			if err != nil {
				if shutdown {
					return
				}
				nonzeroExit = true
				log.Printf("accept tcp connection: %T, %v", err, err)
				break
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer localConn.Close()
				if err := connectToRemoteUnixDomainSocket(localConn, client, flags.RemoteSockAddr); err != nil {
					log.Printf("connect to remote unix domain socket: %v", err)
				}
			}()
		}
		wg.Wait()
	}()

	if flags.CommandName == "" {
		fmt.Printf("%v\n", envPair)
		wg.Wait()
		if nonzeroExit {
			os.Exit(1)
		}
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			shutdown = true
		}()
		cmd := exec.Command(flags.CommandName, flags.CommandArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Env = append(cmd.Env, os.Environ()...)
		cmd.Env = append(cmd.Env, envPair)
		err := cmd.Run()
		shutdown = true
		listener.Close()
		if err != nil {
			log.Println(err)
			nonzeroExit = true
			runtime.Goexit()
		}
	}()
	wg.Wait()
	if nonzeroExit {
		os.Exit(1)
	}
}

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

func connPipe(local, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(local, remote); err != nil {
			log.Printf("copy remote->local: %v", err)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(remote, local); err != nil {
			log.Printf("copy local->remote: %v", err)
		}
	}()
	wg.Wait()
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
