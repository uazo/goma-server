// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"sort"
	"strings"

	"github.com/golang/protobuf/proto"

	gomapb "go.chromium.org/goma/server/proto/api"
	nsjailpb "go.chromium.org/goma/server/proto/nsjail"
)

const (
	nsjailHardeningConfig = `
name: "hardening by nsjail (seccomp-bpf)"
mode: ONCE
# keep_env = true
mount_proc: true
# it runs in docker container, so ok to mount / as RO.
mount <
 src: "/"
 dst: "/"
 is_bind: true
 rw: false
 is_dir: true
>
mount <
 dst: "/tmp"
 fstype: "tmpfs"
 options: "size=5000000"
 rw: true
 is_dir: true
>
# input root is per request, so ok to mount it as RW.
# (does not affect to other requests).
mount <
 prefix_src_env: "INPUT_ROOT"
 src: ""
 prefix_dst_env: "INPUT_ROOT"
 dst: ""
 is_bind: true
 rw: true
 is_dir: true
>
# default may fail with "File too large"
rlimit_fsize_type: INF
rlimit_as_type: INF
# syscalls used by clang.
seccomp_string: "ALLOW {"
seccomp_string: "  access,"
seccomp_string: "  alarm,"
seccomp_string: "  arch_prctl,"
seccomp_string: "  brk,"
seccomp_string: "  close,"
seccomp_string: "  clone,"
seccomp_string: "  connect,"
seccomp_string: "  dup2,"
seccomp_string: "  execve,"
seccomp_string: "  exit_group,"
seccomp_string: "  fcntl,"
seccomp_string: "  futex,"
seccomp_string: "  getcwd,"
seccomp_string: "  getdents,"
seccomp_string: "  getdents64,"
seccomp_string: "  getegid,"
seccomp_string: "  geteuid,"
seccomp_string: "  getgid,"
seccomp_string: "  getpgrp,"
seccomp_string: "  getpid,"
seccomp_string: "  getppid,"
seccomp_string: "  getuid,"
seccomp_string: "  gettid,"
seccomp_string: "  getrlimit,"
seccomp_string: "  ioctl,"
seccomp_string: "  lseek,"
seccomp_string: "  mmap,"
seccomp_string: "  mprotect,"
seccomp_string: "  mremap,"
seccomp_string: "  munmap,"
seccomp_string: "  newfstat,"
seccomp_string: "  newlstat,"
seccomp_string: "  newstat,"
seccomp_string: "  newuname,"
seccomp_string: "  open,"
seccomp_string: "  openat,"
seccomp_string: "  pipe,"
seccomp_string: "  pipe2,"
seccomp_string: "  pread64,"
seccomp_string: "  prlimit64,"
seccomp_string: "  read,"
seccomp_string: "  readlink,"
seccomp_string: "  rename,"
seccomp_string: "  rt_sigaction,"
seccomp_string: "  rt_sigprocmask,"
seccomp_string: "  rt_sigreturn, "
seccomp_string: "  set_robust_list,"
seccomp_string: "  set_tid_address,"
seccomp_string: "  sigaltstack,"
seccomp_string: "  socket,"
seccomp_string: "  sysinfo,"
seccomp_string: "  unlink,"
seccomp_string: "  vfork,"
seccomp_string: "  wait4,"
seccomp_string: "  write,"
seccomp_string: "  writev"
seccomp_string: "}"
seccomp_string: "DEFAULT KILL"
#seccomp_log: true
iface_no_lo: true
`
	nsjailHardeningWrapperScript = `#!/bin/bash
export INPUT_ROOT="$(pwd)"
if [[ "$WORK_DIR" != "" ]]; then
  cd "${WORK_DIR}"
fi
export PWD="$(pwd)"
# exit 159 -> seccomp violation
nsjail -q -C "./nsjail.cfg" --cwd "$PWD" \
       --  \
       "$@"
`

	nsjailChrootRunWrapperScript = `#!/bin/bash
set -e

if [[ "$WORK_DIR" == "" ]]; then
  echo "ERROR: WORK_DIR is not set" >&2
  exit 1
fi

rundir="$(pwd)"
chroot_workdir="/tmp/goma_chroot"

#
# mount directories under $chroot_workdir and execute.
#
run_dirs=($(ls -1 "$rundir"))
sys_dirs=(dev proc)

# RBE server generates __action_home__XXXXXXXXXX directory in $rundir
# (note: XXXXXXXXXX is a random).  Let's skip it because we do not use that.
# mount directories in the request.
for d in "${run_dirs[@]}"; do
  if [[ "$d" == __action_home__* ]]; then
    continue
  fi
  mkdir -p "$chroot_workdir/$d"
  mount --bind "$rundir/$d" "$chroot_workdir/$d"
done

# mount directories not included in the request.
for d in "${sys_dirs[@]}"; do
  # avoid to mount system directories if that exist in the user's request.
  if [[ -d "$rundir/$d" ]]; then
    continue
  fi
  # directory will be mounted by nsjail later.
  mkdir -p "$chroot_workdir/$d"
done
# needed to make nsjail bind device files.
touch "$chroot_workdir/dev/urandom"
touch "$chroot_workdir/dev/null"

# currently running with root. run the command with nobody:nogroup with chroot.
# We use nsjail to chdir without running bash script inside chroot, and
# libc inside chroot can be different from libc outside.
nsjail --quiet --config "$WORK_DIR/nsjail.cfg" -- "$@"
`
)

// pathFromToolchainSpec returns ':'-joined directories of paths in toolchain spec.
// Since symlinks may point to executables, having directories with executables
// may not work, but it is a bit cumbersome to analyze symlinks.
// Also, having library directories in PATH should be harmless because
// the Goma client may not include multiple subprograms with the same name.
func pathFromToolchainSpec(cfp clientFilePath, ts []*gomapb.ToolchainSpec) string {
	m := make(map[string]bool)
	for _, e := range ts {
		m[cfp.Dir(e.GetPath())] = true
	}
	var r []string
	for k := range m {
		if k == "" || k == "." {
			continue
		}
		r = append(r, k)
	}
	// This function must return the same result for the same input, but go
	// does not guarantee the iteration order.
	sort.Strings(r)
	return strings.Join(r, ":")
}

// nsjailConfig returns nsjail configuration.
// When you modify followings, please make sure it matches
// nsjailChrootRunWrapperScript above.
func nsjailChrootConfig(cwd string, cfp clientFilePath, ts []*gomapb.ToolchainSpec, envs []string) []byte {
	chrootWorkdir := "/tmp/goma_chroot"
	cfg := &nsjailpb.NsJailConfig{
		Uidmap: []*nsjailpb.IdMap{
			{
				InsideId:  proto.String("nobody"),
				OutsideId: proto.String("nobody"),
			},
		},
		Gidmap: []*nsjailpb.IdMap{
			{
				InsideId:  proto.String("nogroup"),
				OutsideId: proto.String("nogroup"),
			},
		},
		Mount: []*nsjailpb.MountPt{
			{
				Src:    proto.String(chrootWorkdir),
				Dst:    proto.String("/"),
				IsBind: proto.Bool(true),
				Rw:     proto.Bool(true),
				IsDir:  proto.Bool(true),
			},
			{
				Src:    proto.String("/dev/null"),
				Dst:    proto.String("/dev/null"),
				Rw:     proto.Bool(true),
				IsBind: proto.Bool(true),
			},
			{
				Src:    proto.String("/dev/urandom"),
				Dst:    proto.String("/dev/urandom"),
				IsBind: proto.Bool(true),
			},
		},
		Cwd: proto.String(cwd),
		// TODO: use log file and print to server log.
		LogLevel:  nsjailpb.LogLevel_WARNING.Enum(),
		MountProc: proto.Bool(true),
		Envar: append(
			[]string{
				"PATH=" + pathFromToolchainSpec(cfp, ts),
				// Dummy home directory is needed by pnacl-clang to
				// import site.py to import user-defined python
				// packages.
				"HOME=/",
			},
			// Add client-side environemnt to execution environment.
			envs...),
		RlimitAsType:    nsjailpb.RLimit_INF.Enum(),
		RlimitFsizeType: nsjailpb.RLimit_INF.Enum(),
		// TODO: relax RLimit from the default.
		// Default size might be too strict, and not suitable for
		// compiling.
	}
	return []byte(proto.MarshalTextString(cfg))
}
