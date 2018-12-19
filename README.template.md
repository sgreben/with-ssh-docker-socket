# ${APP}

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
$ ${APP} -i key.pem -a user@remote-host docker ps
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
$ ${APP} -a user@remote-host docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

### Running a shell

If no command is specified, the current `$SHELL` will be run as a child process of `${APP}`:
```sh
$ ${APP} -a user@remote-host
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
$ ${APP} -a user@remote-host bash
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

- `-ssh-app-openssh`: `ssh -nNT -L "{{.LocalPort}}:{{.RemoteAddr}}" -p "{{.SSHPort}}"  "{{.User}}@{{.SSHHost}}" {{.ExtraArgs}}`
- `-ssh-app-putty`: `putty -ssh -NT "{{.User}}@{{.SSHHost}}" -P "{{.SSHPort}}"  -L "{{.LocalPort}}:{{.RemoteAddr}}" {{.ExtraArgs}}`
- `-ssh-app=<TEMPLATE>`: where `TEMPLATE` is a go template that may refer to the same variables as the built-in templates `-ssh-app-openssh` and `-ssh-app-putty`.

```sh
$ ${APP} -ssh-app-openssh -a user@remote-host docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

The same result using a custom template:

```sh
$ ${APP} -ssh-app='ssh -nNT -L "{{.LocalPort}}:{{.RemoteSocketAddr}}" "{{.RemoteHost}}"' -a user@remote-host docker ps
```
```sh
CONTAINER ID  IMAGE                       COMMAND               CREATED      STATUS
4b56090ce1bb  google/cadvisor:v0.31.0     "/usr/bin/cadvisor…"  1 hour ago   Up 1 hour
```

## Get it

### Using `go get`

```sh
go get -u github.com/sgreben/${APP}
```

### Pre-built binary

Or [download a binary](https://github.com/sgreben/${APP}/releases/latest) from the releases page, or from the shell:

```sh
# Linux
curl -L https://github.com/sgreben/${APP}/releases/download/${VERSION}/${APP}_${VERSION}_linux_x86_64.tar.gz | tar xz

# OS X
curl -L https://github.com/sgreben/${APP}/releases/download/${VERSION}/${APP}_${VERSION}_osx_x86_64.tar.gz | tar xz

# Windows
curl -LO https://github.com/sgreben/${APP}/releases/download/${VERSION}/${APP}_${VERSION}_windows_x86_64.zip
unzip ${APP}_${VERSION}_windows_x86_64.zip
```

## Use it

```text
${APP} [OPTIONS] [COMMAND [ARGS...]]
```

```text
${USAGE}
```
