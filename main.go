package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/sgreben/sshtunnel"
	"github.com/sgreben/sshtunnel/backoff"
	sshtunnelExec "github.com/sgreben/sshtunnel/exec"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var flags struct {
	SSHUser                    string
	SSHKeyPath                 string
	SSHKeyPass                 string
	SSHAddr                    string
	SSHHost                    string
	SSHPort                    string
	SSHExternalClient          string
	SSHExternalClientOpenSSH   bool
	SSHExternalClientPuTTY     bool
	SSHExternalClientExtraArgs string
	RemoteSocketAddr           string
	SSHAuthSocketAddr          string
	LocalListenIP              string
	LocalListenPort            int
	EnvVarName                 string
	CommandName                string
	CommandArgs                []string
	Verbose                    bool
	BackoffConfig              backoff.Config
}

var state struct {
	sshKey     ssh.Signer
	sshAgent   agent.Agent
	listenAddr *net.TCPAddr

	listener net.Listener
}

const appName = "with-ssh-docker-socket"

var version = "SNAPSHOT"
var nonzeroExit bool

func init() {
	flags.SSHAuthSocketAddr = os.Getenv("SSH_AUTH_SOCK")
	flags.SSHUser = os.Getenv("USER")
	flags.RemoteSocketAddr = "/var/run/docker.sock"
	flags.LocalListenIP = "127.0.0.1"
	flags.EnvVarName = "DOCKER_HOST"
	flags.LocalListenPort = 0
	flags.BackoffConfig.Min = 250 * time.Millisecond
	flags.BackoffConfig.Max = 15 * time.Second
	flags.BackoffConfig.MaxAttempts = 10

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
	flag.StringVar(&flags.SSHExternalClient, "ssh-app", flags.SSHExternalClient, "use an external ssh client application (default: use native (go) ssh client)")
	flag.StringVar(&flags.SSHExternalClientExtraArgs, "ssh-app-extra-args", flags.SSHExternalClientExtraArgs, "extra CLI arguments for external ssh clients")
	flag.BoolVar(&flags.SSHExternalClientOpenSSH, "ssh-app-openssh", flags.SSHExternalClientOpenSSH, fmt.Sprintf("use the openssh `ssh` CLI (%q) (default: use native (go) ssh client)", sshtunnelExec.CommandTemplateOpenSSHText))
	flag.BoolVar(&flags.SSHExternalClientPuTTY, "ssh-app-putty", flags.SSHExternalClientPuTTY, fmt.Sprintf("use the PuTTY CLI (%q)  (default: use native (go) ssh client)", sshtunnelExec.CommandTemplatePuTTYText))
	flag.DurationVar(&flags.BackoffConfig.Max, "ssh-max-delay", flags.BackoffConfig.Max, "maximum re-connection attempt delay")
	flag.DurationVar(&flags.BackoffConfig.Min, "ssh-min-delay", flags.BackoffConfig.Min, "minimum re-connection attempt delay")
	flag.IntVar(&flags.BackoffConfig.MaxAttempts, "ssh-max-attempts", flags.BackoffConfig.MaxAttempts, "maximum number of ssh re-connection attempts")

	flag.Parse()

	if flags.SSHAddr == "" {
		flag.Usage()
		log.Fatal("error: no ssh server address specified (-ssh-server-addr / -a)")
	}

	flags.SSHHost = flags.SSHAddr
	flags.SSHPort = "22"
	if i := strings.IndexRune(flags.SSHAddr, '@'); i >= 0 {
		flags.SSHUser, flags.SSHHost = flags.SSHAddr[:i], flags.SSHAddr[i+1:]
	}
	if host, port, err := net.SplitHostPort(flags.SSHHost); err == nil {
		flags.SSHHost, flags.SSHPort = host, port
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

	if flags.CommandName == "" {
		flags.CommandName = os.Getenv("SHELL")
	}

	if flags.CommandName == "" {
		log.Fatal("no command specified, and no $SHELL defined")
	}

	if flags.SSHExternalClientOpenSSH {
		flags.SSHExternalClient = sshtunnelExec.CommandTemplateOpenSSHText
	}
	if flags.SSHExternalClientPuTTY {
		flags.SSHExternalClient = sshtunnelExec.CommandTemplatePuTTYText
	}
	if flags.SSHExternalClient != "" {
		useSSHClientExternal()
		return
	}
	useSSHClientNative()
}

func useSSHClientNative() {
	var authConfig sshtunnel.ConfigAuth
	if flags.SSHKeyPath != "" {
		key := sshtunnel.KeySource{
			Path: &flags.SSHKeyPath,
		}
		if flags.SSHKeyPass != "" {
			passphrase := []byte(flags.SSHKeyPass)
			key.Passphrase = &passphrase
		}
		authConfig.Keys = append(authConfig.Keys, key)
	}

	if flags.SSHAuthSocketAddr != "" {
		authConfig.SSHAgent = &sshtunnel.ConfigSSHAgent{
			Addr: &net.UnixAddr{
				Net:  "unix",
				Name: flags.SSHAuthSocketAddr,
			},
		}
	}
	auth, err := authConfig.Methods()
	if err != nil {
		log.Fatalf("tunnel auth setup failed: %v", err)
	}
	clientConfig := &ssh.ClientConfig{
		User:            flags.SSHUser,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	tunnelConfig := &sshtunnel.Config{
		SSHAddr:   flags.SSHHost + ":" + flags.SSHPort,
		SSHClient: clientConfig,
	}
	listener, errCh, err := sshtunnel.Listen(
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
		"unix",
		flags.RemoteSocketAddr,
		tunnelConfig,
		flags.BackoffConfig,
	)
	if err != nil {
		log.Fatalf("tunnel connection failed: %v", err)
	}
	go func() {
		err := <-errCh
		log.Fatalf("tunnel connection failed: %v", err)
	}()
	state.listener = listener
}

func useSSHClientExternal() {
	template := template.Must(template.New("").Parse(flags.SSHExternalClient))
	tunnelConfig := &sshtunnelExec.Config{
		User:             flags.SSHUser,
		SSHHost:          flags.SSHHost,
		SSHPort:          flags.SSHPort,
		CommandTemplate:  template,
		CommandExtraArgs: flags.SSHExternalClientExtraArgs,
		Backoff:          flags.BackoffConfig,
	}
	listener, errCh, err := sshtunnelExec.Listen(
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
		flags.RemoteSocketAddr,
		tunnelConfig,
	)
	if err != nil {
		log.Fatalf("tunnel connection failed: %v", err)
	}
	go func() {
		err := <-errCh
		log.Fatalf("tunnel connection failed: %v", err)
	}()
	state.listener = listener
}

func main() {
	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-signals
		log.Printf("received %v signal, shutting down", s)
		state.listener.Close()
	}()

	if flags.Verbose {
		log.Printf("forwarding %v to socket %q on %v", state.listener.Addr(), flags.RemoteSocketAddr, flags.SSHAddr)
	}
	envKeyValuePair := fmt.Sprintf("%v=tcp://%v", flags.EnvVarName, state.listener.Addr())

	cmd := exec.Command(flags.CommandName, flags.CommandArgs...)
	if flags.Verbose {
		log.Printf("exec: [%v] %v", envKeyValuePair, cmd.Args)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, envKeyValuePair)
	if err := cmd.Run(); err != nil {
		nonzeroExit = true
	}
	if nonzeroExit {
		os.Exit(1)
	}
}
