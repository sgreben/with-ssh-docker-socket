package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var flags struct {
	SSHUserName              string
	SSHKeyPath               string
	SSHKeyPass               string
	SSHAddr                  string
	SSHHost                  string
	SSHExternalClient        string
	SSHExternalClientOpenSSH bool
	SSHExternalClientPuTTY   bool
	RemoteSocketAddr         string
	SSHAuthSocketAddr        string
	LocalListenIP            string
	LocalListenPort          int
	EnvVarName               string
	CommandName              string
	CommandArgs              []string
	Verbose                  bool
}

var state struct {
	sshKey     ssh.Signer
	sshAgent   agent.Agent
	listenAddr *net.TCPAddr

	client          *ssh.Client
	listener        *net.TCPListener
	shutdown        bool
	envKeyValuePair string
	connect         func(net.Conn) error
}

const appName = "with-ssh-docker-socket"

var version = "SNAPSHOT"
var nonzeroExit bool

func init() {
	flags.SSHAuthSocketAddr = os.Getenv("SSH_AUTH_SOCK")
	flags.SSHUserName = os.Getenv("USER")
	flags.RemoteSocketAddr = "/var/run/docker.sock"
	flags.LocalListenIP = "127.0.0.1"
	flags.EnvVarName = "DOCKER_HOST"
	flags.LocalListenPort = 0

	log.SetOutput(os.Stderr)
	log.SetPrefix(fmt.Sprintf("[%s] ", appName))
	log.SetFlags(0)
	flag.StringVar(&flags.SSHAuthSocketAddr, "ssh-auth-sock", flags.SSHAuthSocketAddr, "ssh-agent socket address ($SSH_AUTH_SOCK)")
	flag.StringVar(&flags.SSHKeyPath, "ssh-key-file", flags.SSHKeyPath, "path of an ssh key file")
	flag.StringVar(&flags.SSHKeyPath, "i", flags.SSHKeyPath, "(alias for -ssh-key-file)")
	flag.StringVar(&flags.SSHKeyPass, "ssh-key-pass", flags.SSHKeyPass, "passphrase for the ssh key file given via `-i`")
	flag.StringVar(&flags.RemoteSocketAddr, "remote-socket-path", flags.RemoteSocketAddr, "remote socket path")
	flag.StringVar(&flags.RemoteSocketAddr, "s", flags.RemoteSocketAddr, "(alias for -remote-socket-path)")
	flag.StringVar(&flags.LocalListenIP, "listen-ip", flags.LocalListenIP, "local IP to listen on")
	flag.IntVar(&flags.LocalListenPort, "listen-port", flags.LocalListenPort, "local TCP port to listen on (set to 0 to assign a random free port)")
	flag.IntVar(&flags.LocalListenPort, "p", flags.LocalListenPort, "(alias for -listen-port)")
	flag.StringVar(&flags.SSHAddr, "ssh-server-addr", flags.SSHAddr, "(remote) ssh server address [user@]host[:port]")
	flag.StringVar(&flags.SSHAddr, "a", flags.SSHAddr, "(alias for -ssh-server-addr)")
	flag.StringVar(&flags.EnvVarName, "env-var-name", flags.EnvVarName, "environment variable to set")
	flag.StringVar(&flags.EnvVarName, "e", flags.EnvVarName, "(alias for -env-var-name)")
	flag.BoolVar(&flags.Verbose, "verbose", flags.Verbose, "print more logs")
	flag.BoolVar(&flags.Verbose, "v", flags.Verbose, "(alias for -verbose)")
	flag.StringVar(&flags.SSHExternalClient, "ssh-app", flags.SSHExternalClient, "use an external ssh client application (default: use built-in ssh client)")
	flag.BoolVar(&flags.SSHExternalClientOpenSSH, "ssh-app-openssh", flags.SSHExternalClientOpenSSH, fmt.Sprintf("use the openssh `ssh` CLI (%q) (default: use built-in ssh client)", sshClientTemplateOpenSSHText))
	flag.BoolVar(&flags.SSHExternalClientPuTTY, "ssh-app-putty", flags.SSHExternalClientPuTTY, fmt.Sprintf("use the PuTTY CLI (%q)  (default: use built-in ssh client)", sshClientTemplatePuTTYText))
	flag.Parse()

	if flags.SSHAddr == "" {
		flag.Usage()
		log.Fatal("error: no ssh server address specified (-ssh-server-addr / -a)")
	}

	flags.SSHHost = flags.SSHAddr
	if i := strings.IndexRune(flags.SSHAddr, '@'); i >= 0 {
		flags.SSHUserName, flags.SSHHost = flags.SSHAddr[:i], flags.SSHAddr[i+1:]
	}

	if flag.NArg() > 0 {
		flags.CommandName = flag.Arg(0)
	}
	if flag.NArg() > 1 {
		flags.CommandArgs = flag.Args()[1:]
	}

	state.listenAddr = &net.TCPAddr{
		IP:   net.ParseIP(flags.LocalListenIP),
		Port: flags.LocalListenPort,
	}

	if flags.SSHExternalClientOpenSSH {
		flags.SSHExternalClient = sshClientTemplateOpenSSHText
	}
	if flags.SSHExternalClientPuTTY {
		flags.SSHExternalClient = sshClientTemplatePuTTYText
	}
	if flags.SSHExternalClient != "" {
		initSSHClientExternal()
		return
	}
	initSSHClientBuiltin()
}

func initSSHClientBuiltin() {
	if flags.SSHKeyPath != "" {
		keyBytes, err := ioutil.ReadFile(flags.SSHKeyPath)
		if err != nil {
			log.Fatalf("read key file %q: %v", flags.SSHKeyPath, err)
		}
		state.sshKey, err = func() (ssh.Signer, error) {
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

	if state.sshKey == nil && flags.SSHAuthSocketAddr != "" {
		conn, err := localSSHAgent(flags.SSHAuthSocketAddr)
		if err != nil {
			log.Printf("connect to ssh-agent %q: %v", flags.SSHAuthSocketAddr, err)
		} else {
			state.sshAgent = agent.NewClient(conn)
		}
	}

	if flags.Verbose {
		log.Printf("connecting to %v@%v", flags.SSHUserName, flags.SSHAddr)
	}
	client, err := connectSSH(flags.SSHUserName, state.sshKey, state.sshAgent, flags.SSHHost)
	if err != nil {
		log.Fatal(err)
	}
	state.client = client

	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("open ssh session: %v", err)
	}
	defer session.Close()
	state.connect = func(localConn net.Conn) error {
		return connectToRemoteUnixDomainSocket(localConn, state.client, flags.RemoteSocketAddr)
	}

}

func initSSHClientExternal() {
	template, err := sshClientTemplate.Parse(flags.SSHExternalClient)
	if err != nil {
		log.Fatalf("parse external ssh client command template: %v", err)
	}
	state.connect = func(localConn net.Conn) error {
		return connectToRemoteUnixDomainSocketExternalClient(localConn, template)
	}
}

func main() {
	var err error

	const tcpNet = "tcp"
	state.listener, err = net.ListenTCP(tcpNet, state.listenAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer state.listener.Close()

	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-signals
		log.Printf("received %v signal, shutting down", s)
		state.shutdown = true
		state.listener.Close()
	}()

	if flags.Verbose {
		log.Printf("forwarding %v to socket %q on %v", state.listener.Addr(), flags.RemoteSocketAddr, flags.SSHAddr)
	}
	state.envKeyValuePair = fmt.Sprintf("%v=tcp://%v", flags.EnvVarName, state.listener.Addr())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		acceptConnections(state.connect)
	}()

	if flags.CommandName == "" {
		flags.CommandName = os.Getenv("SHELL")
	}

	if flags.CommandName == "" {
		fmt.Printf("%v\n", state.envKeyValuePair)
		wg.Wait()
		if nonzeroExit {
			os.Exit(1)
		}
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := runCommand(); err != nil {
			nonzeroExit = true
			log.Println(err)
		}
		shutdown()
		state.listener.Close()
	}()
	wg.Wait()
	if nonzeroExit {
		os.Exit(1)
	}
}

func shutdown() { state.shutdown = true }

func acceptConnections(connect func(net.Conn) error) {
	var wg sync.WaitGroup
	for {
		localConn, err := state.listener.Accept()
		if err != nil {
			if state.shutdown {
				break
			}
			nonzeroExit = true
			log.Printf("accept tcp connection: %T, %v", err, err)
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer localConn.Close()
			if err := connect(localConn); err != nil {
				log.Printf("connect to remote unix domain socket: %v", err)
			}
		}()
	}
	wg.Wait()
}

func runCommand() error {
	cmd := exec.Command(flags.CommandName, flags.CommandArgs...)
	if flags.Verbose {
		log.Printf("exec: %v", cmd.Args)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, state.envKeyValuePair)
	return cmd.Run()
}
