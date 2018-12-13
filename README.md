# with-ssh-docker-socket

Access a remote Docker daemon over SSH.

More precisely, this tool does the following:

1. Establish an SSH connection
2. Forward the remote Docker socket to a local TCP port
3. Run the given command (e.g. `docker build` or `docker-compose up`) with the `DOCKER_HOST` environment variable set to the forwarded socket
4. Close the SSH connection after the command exits

## Contents

- [Contents](#contents)
- [Example](#example)
  - [Basic usage](#basic-usage)
  - [Running a shell](#running-a-shell)
  - [External SSH client applications](#external-ssh-client-applications)
- [Get it](#get-it)
  - [Using `go get`](#using-go-get)
  - [Pre-built binary](#pre-built-binary)
- [Use it](#use-it)

## Example

### Basic usage

The following command runs `docker ps` against the Docker daemon on host `remote-host`.
Note that the `docker` CLI **client** being run here is the **local** one, whereas the **daemon** `dockerd` is running **remotely** on `remote-host`.

```sh
$ with-ssh-docker-socket -i key.pem -a user@remote-host docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

If `ssh-agent` is running and unlocked, its keyring will be used:
```sh
$ ssh-add key.pem
```
```sh
$ with-ssh-docker-socket -a user@remote-host docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

### Running a shell

If no command is specified, the current `$SHELL` will be run as a child process of `with-ssh-docker-socket`:
```sh
$ with-ssh-docker-socket -a user@remote-host
```
```sh
$ docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```
```sh
$ exit
```
```sh
$ docker ps
Cannot connect to the Docker daemon at localhost. Is the docker daemon running?
```

Of course, you can also just explicitly specify a shell as the command to run:
```sh
$ with-ssh-docker-socket -a user@remote-host bash
```
```sh
bash-3.2$ docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

### External SSH client applications

> **Note**: Using an external ssh client introduces additional dependencies - the client itself, as well its configuration (e.g. the contents of `~/.ssh/config`). This makes the tool no longer self-contained, and its effect less obvious. For these reasons I'd recommend against the usage of this feature for automation puproses.

If you wish to use a pre-installed external ssh client (such as **openssh** or **PuTTY**), you may use the `-ssh-app` options. There are two shortcut flags specifically for **openssh** and **PuTTY**, as well as a way to call a custom client application:

- `-ssh-app-openssh`: `ssh -nNT -L "{{.LocalPort}}:{{.RemoteSocketAddr}}" "{{.RemoteHost}}"`
- `-ssh-app-putty`: `putty -ssh -NT "{{.RemoteHost}}" -L "{{.LocalPort}}:{{.RemoteSocketAddr}}"`
- `-ssh-app=<TEMPLATE>`: where `TEMPLATE` is a go template that may refer to the same variables as the built-in templates `-ssh-app-openssh` and `-ssh-app-putty`.

```sh
$ with-ssh-docker-socket -ssh-app-openssh -a user@remote-host docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

The same result using a custom template:

```sh
$ with-ssh-docker-socket -ssh-app='ssh -nNT -L "{{.LocalPort}}:{{.RemoteSocketAddr}}" "{{.RemoteHost}}"' -a user@remote-host docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

## Get it

### Using `go get`

```sh
go get -u github.com/sgreben/with-ssh-docker-socket
```

### Pre-built binary

Or [download a binary](https://github.com/sgreben/with-ssh-docker-socket/releases/latest) from the releases page, or from the shell:

```sh
# Linux
curl -L https://github.com/sgreben/with-ssh-docker-socket/releases/download/1.2.2/with-ssh-docker-socket_1.2.2_linux_x86_64.tar.gz | tar xz

# OS X
curl -L https://github.com/sgreben/with-ssh-docker-socket/releases/download/1.2.2/with-ssh-docker-socket_1.2.2_osx_x86_64.tar.gz | tar xz

# Windows
curl -LO https://github.com/sgreben/with-ssh-docker-socket/releases/download/1.2.2/with-ssh-docker-socket_1.2.2_windows_x86_64.zip
unzip with-ssh-docker-socket_1.2.2_windows_x86_64.zip
```

## Use it

```text
with-ssh-docker-socket [OPTIONS] [COMMAND [ARGS...]]
```

```text
Usage of with-ssh-docker-socket:
  -a string
    	(alias for -ssh-server-addr)
  -e string
    	(alias for -env-var-name) (default "DOCKER_HOST")
  -env-var-name string
    	environment variable to set (default "DOCKER_HOST")
  -i string
    	(alias for -ssh-key-file)
  -listen-ip string
    	local IP to listen on (default "127.0.0.1")
  -listen-port int
    	local TCP port to listen on (set to 0 to assign a random free port)
  -p int
    	(alias for -listen-port)
  -remote-socket-path string
    	remote socket path (default "/var/run/docker.sock")
  -s string
    	(alias for -remote-socket-path) (default "/var/run/docker.sock")
  -ssh-app string
    	use an external ssh client application (default: use built-in ssh client)
  -ssh-app-openssh ssh
    	use the openssh ssh CLI ("ssh -nNT -L \"{{.LocalPort}}:{{.RemoteSocketAddr}}\" \"{{.RemoteHost}}\"") (default: use built-in ssh client)
  -ssh-app-putty
    	use the PuTTY CLI ("putty -ssh -NT \"{{.RemoteHost}}\" -L \"{{.LocalPort}}:{{.RemoteSocketAddr}}\"")  (default: use built-in ssh client)
  -ssh-auth-sock string
    	ssh-agent socket address ($SSH_AUTH_SOCK)
  -ssh-key-file string
    	path of an ssh key file
  -ssh-key-pass -i
    	passphrase for the ssh key file given via -i
  -ssh-server-addr string
    	(remote) ssh server address [user@]host[:port]
  -v	(alias for -verbose)
  -verbose
    	print more logs
```
