# Running gVisor (`runsc`) sandbox tests on macOS

gVisor needs a Linux kernel, so on macOS the sandbox runs inside a Linux VM. This guide sets up a **dedicated Colima profile** (Colima runs Lima underneath) with Docker and gVisor `runsc`, and leaves your existing Docker setup unchanged. See [ARCHITECTURE.md §15.4](../ARCHITECTURE.md#154-local-and-ci-execution) for how `runsc` is used in testing.

The dedicated profile is called `agenty-runsc` throughout. Its Docker socket is `~/.colima/agenty-runsc/docker.sock`.

## Prerequisites

- Apple silicon or Intel Mac on macOS 13 or later (the `vz` VM type needs macOS 13).
- Colima, Lima, and the Docker CLI on the `PATH`, for example `brew install colima lima docker`.
- Nothing is installed on the macOS host beyond these; `runsc` is installed inside the VM.

## 1. Create the dedicated Colima profile

`colima start` creates a Docker context for the profile **and switches the active Docker context to it**. To keep your current context (for example the `default` Colima profile) active, run it with a throwaway Docker config directory:

```sh
mkdir -p /tmp/agenty-runsc-dockercfg
DOCKER_CONFIG=/tmp/agenty-runsc-dockercfg \
  colima start --profile agenty-runsc --cpu 2 --memory 2 --disk 20 --vm-type vz --runtime docker
docker context show   # still your previous context
```

If you prefer the profile to be your active context, omit `DOCKER_CONFIG`; Colima then switches you to the `colima-agenty-runsc` context.

## 2. Install `runsc` inside the VM

The VM is Ubuntu. Install `runsc` from gVisor's apt repository (see [gVisor installation](https://gvisor.dev/docs/user_guide/install/)). Open a shell in the VM with `colima ssh --profile agenty-runsc` and run:

```sh
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg
curl -fsSL https://gvisor.dev/archive.key \
  | sudo gpg --dearmor --yes -o /usr/share/keyrings/gvisor-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" \
  | sudo tee /etc/apt/sources.list.d/gvisor.list
sudo apt-get update
sudo apt-get install -y runsc
runsc --version
exit
```

The `runsc` package registers itself as a Docker runtime in `/etc/docker/daemon.json`. The direct-download URLs under `releases/release/latest/<arch>/runsc` shown in older instructions no longer exist (that directory now holds only tarballs); use the apt repository.

## 3. Register the runtime in the Colima profile

Colima regenerates `/etc/docker/daemon.json` from its own configuration on every start, which drops the `runsc` entry after a restart. Declare the runtime in the profile configuration instead. In `~/.colima/agenty-runsc/colima.yaml`, replace `docker: {}` with:

```yaml
docker:
  runtimes:
    runsc:
      path: /usr/bin/runsc
```

Then restart the profile so Docker picks it up:

```sh
colima stop --profile agenty-runsc
DOCKER_CONFIG=/tmp/agenty-runsc-dockercfg colima start --profile agenty-runsc
colima ssh --profile agenty-runsc -- cat /etc/docker/daemon.json   # contains "runsc"
```

## 4. Verify a container runs under gVisor

Address the profile's Docker daemon explicitly with `-H` (or `--context colima-agenty-runsc` if you let Colima create the context in your normal Docker config):

```sh
SOCK=unix://$HOME/.colima/agenty-runsc/docker.sock
docker -H "$SOCK" run --rm --runtime=runsc hello-world
docker -H "$SOCK" run --rm --runtime=runsc alpine:3 dmesg
docker -H "$SOCK" run --rm --runtime=runsc alpine:3 uname -r
docker -H "$SOCK" run --rm alpine:3 uname -r
```

Expected:

- `hello-world` prints `Hello from Docker!`.
- `dmesg` starts with `Starting gVisor...` followed by gVisor's boot messages; a `runc` container shows the VM's real kernel log instead.
- `uname -r` reports a gVisor kernel version (for example `4.19.0-gvisor`) under `runsc`, and the VM's Linux kernel (for example `6.8.0-117-generic`) under the default `runc`.

## 5. Running the sandbox tests

Point Docker at the profile when running the Task test targets, for example `DOCKER_HOST=unix://$HOME/.colima/agenty-runsc/docker.sock task test:module`. Without a `runsc` runtime, sandbox tests fall back to `insecure_dev_mode` and report that clearly (§15.4).

## Stopping and removing

```sh
colima stop --profile agenty-runsc     # keeps the VM and runsc for next time
colima delete --profile agenty-runsc   # removes the VM entirely
```

Neither command touches other Colima profiles or Docker contexts.

## Verification record

| Item | Value |
|---|---|
| Date | 2026-10-05 (2026-10-04T22:32:37Z) |
| macOS | 26.5.1 (build 25F80), arm64 |
| Colima | 0.10.3 (`vz`, aarch64) |
| Lima (`limactl`) | 2.2.0 |
| VM | Ubuntu 24.04.4 LTS, kernel 6.8.0-117-generic |
| Docker | client 29.8.2, server 29.5.2 (inside the VM) |
| gVisor `runsc` | release-20260928.0 (OCI spec 1.2.1), platform `systrap` |

Steps 1–4 were followed on a fresh `agenty-runsc` profile, including a profile restart after step 3. Step 2 was run as individual `colima ssh --profile agenty-runsc -- <command>` calls instead of an interactive shell: the key was downloaded to a file before `gpg --dearmor`, and the apt source file was copied in with `limactl copy`. The result is the same as the pipes shown above. Observed output:

```text
$ docker -H "$SOCK" run --rm --runtime=runsc hello-world
Hello from Docker!
This message shows that your installation appears to be working correctly.
...
$ docker -H "$SOCK" run --rm --runtime=runsc alpine:3 dmesg
[   0.000000] Starting gVisor...
...
[   3.409528] Ready!
$ docker -H "$SOCK" run --rm --runtime=runsc alpine:3 uname -r
4.19.0-gvisor
$ docker -H "$SOCK" run --rm alpine:3 uname -r
6.8.0-117-generic
```

Without step 3, `/etc/docker/daemon.json` lost the `runsc` entry after `colima stop`/`colima start`; with it, the entry persisted. The developer's active Docker context (`colima`, the default profile) was unchanged throughout. After verification the `agenty-runsc` profile was stopped with `colima stop --profile agenty-runsc` and kept (not deleted).
