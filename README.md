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
- [Get it](#get-it)
  - [Using `go get`](#using-go-get)
  - [Pre-built binary](#pre-built-binary)
- [Use it](#use-it)

## Example

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

## Get it

### Using `go get`

```sh
go get -u github.com/sgreben/with-ssh-docker-socket
```

### Pre-built binary

Or [download a binary](https://github.com/sgreben/with-ssh-docker-socket/releases/latest) from the releases page, or from the shell:

```sh
# Linux
curl -L https://github.com/sgreben/with-ssh-docker-socket/releases/download/1.0.2/with-ssh-docker-socket_1.0.2_linux_x86_64.tar.gz | tar xz

# OS X
curl -L https://github.com/sgreben/with-ssh-docker-socket/releases/download/1.0.2/with-ssh-docker-socket_1.0.2_osx_x86_64.tar.gz | tar xz

# Windows
curl -LO https://github.com/sgreben/with-ssh-docker-socket/releases/download/1.0.2/with-ssh-docker-socket_1.0.2_windows_x86_64.zip
unzip with-ssh-docker-socket_1.0.2_windows_x86_64.zip
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
  -ssh-auth-sock string
    	ssh-agent socket address ($SSH_AUTH_SOCK)
  -ssh-key-file string
    	path of an ssh key file
  -ssh-key-pass -i
    	passphrase for the ssh key file given via -i
  -ssh-server-addr string
    	(remote) ssh server address
  -ssh-user string
    	ssh user name
  -u string
    	(alias for -ssh-user)
  -v	(alias for -verbose)
  -verbose
    	print more logs
```
