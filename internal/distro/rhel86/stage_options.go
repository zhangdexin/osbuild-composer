package rhel86

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"

	"github.com/osbuild/osbuild-composer/internal/blueprint"
	"github.com/osbuild/osbuild-composer/internal/common"
	"github.com/osbuild/osbuild-composer/internal/crypt"
	"github.com/osbuild/osbuild-composer/internal/disk"
	"github.com/osbuild/osbuild-composer/internal/distro"
	osbuild "github.com/osbuild/osbuild-composer/internal/osbuild2"
	"github.com/osbuild/osbuild-composer/internal/rpmmd"
)

const (
	kspath = "/osbuild.ks"
)

func rpmStageOptions(repos []rpmmd.RepoConfig) *osbuild.RPMStageOptions {
	var gpgKeys []string
	for _, repo := range repos {
		if repo.GPGKey == "" {
			continue
		}
		gpgKeys = append(gpgKeys, repo.GPGKey)
	}

	return &osbuild.RPMStageOptions{
		GPGKeys: gpgKeys,
	}
}

// selinuxStageOptions returns the options for the org.osbuild.selinux stage.
// Setting the argument to 'true' relabels the '/usr/bin/cp' and '/usr/bin/tar'
// binaries with 'install_exec_t'. This should be set in the build root.
func selinuxStageOptions(labelcp bool) *osbuild.SELinuxStageOptions {
	options := &osbuild.SELinuxStageOptions{
		FileContexts: "etc/selinux/targeted/contexts/files/file_contexts",
	}
	if labelcp {
		options.Labels = map[string]string{
			"/usr/bin/cp":  "system_u:object_r:install_exec_t:s0",
			"/usr/bin/tar": "system_u:object_r:install_exec_t:s0",
		}
	}
	return options
}

func userStageOptions(users []blueprint.UserCustomization) (*osbuild.UsersStageOptions, error) {
	options := osbuild.UsersStageOptions{
		Users: make(map[string]osbuild.UsersStageOptionsUser),
	}

	for _, c := range users {
		if c.Password != nil && !crypt.PasswordIsCrypted(*c.Password) {
			cryptedPassword, err := crypt.CryptSHA512(*c.Password)
			if err != nil {
				return nil, err
			}

			c.Password = &cryptedPassword
		}

		user := osbuild.UsersStageOptionsUser{
			Groups:      c.Groups,
			Description: c.Description,
			Home:        c.Home,
			Shell:       c.Shell,
			Password:    c.Password,
			Key:         c.Key,
		}

		user.UID = c.UID
		user.GID = c.GID

		options.Users[c.Name] = user
	}

	return &options, nil
}

func usersFirstBootOptions(usersStageOptions *osbuild.UsersStageOptions) *osbuild.FirstBootStageOptions {
	cmds := make([]string, 0, 3*len(usersStageOptions.Users)+1)
	// workaround for creating authorized_keys file for user
	varhome := filepath.Join("/var", "home")
	for name, user := range usersStageOptions.Users {
		if user.Key != nil {
			sshdir := filepath.Join(varhome, name, ".ssh")
			cmds = append(cmds, fmt.Sprintf("mkdir -p %s", sshdir))
			cmds = append(cmds, fmt.Sprintf("sh -c 'echo %q >> %q'", *user.Key, filepath.Join(sshdir, "authorized_keys")))
			cmds = append(cmds, fmt.Sprintf("chown %s:%s -Rc %s", name, name, sshdir))
		}
	}
	cmds = append(cmds, fmt.Sprintf("restorecon -rvF %s", varhome))
	options := &osbuild.FirstBootStageOptions{
		Commands:       cmds,
		WaitForNetwork: false,
	}

	return options
}

func groupStageOptions(groups []blueprint.GroupCustomization) *osbuild.GroupsStageOptions {
	options := osbuild.GroupsStageOptions{
		Groups: map[string]osbuild.GroupsStageOptionsGroup{},
	}

	for _, group := range groups {
		groupData := osbuild.GroupsStageOptionsGroup{
			Name: group.Name,
		}
		groupData.GID = group.GID

		options.Groups[group.Name] = groupData
	}

	return &options
}

func firewallStageOptions(firewall *blueprint.FirewallCustomization) *osbuild.FirewallStageOptions {
	options := osbuild.FirewallStageOptions{
		Ports: firewall.Ports,
	}

	if firewall.Services != nil {
		options.EnabledServices = firewall.Services.Enabled
		options.DisabledServices = firewall.Services.Disabled
	}

	return &options
}

func systemdStageOptions(enabledServices, disabledServices []string, s *blueprint.ServicesCustomization, target string) *osbuild.SystemdStageOptions {
	if s != nil {
		enabledServices = append(enabledServices, s.Enabled...)
		disabledServices = append(disabledServices, s.Disabled...)
	}
	return &osbuild.SystemdStageOptions{
		EnabledServices:  enabledServices,
		DisabledServices: disabledServices,
		DefaultTarget:    target,
	}
}

func buildStampStageOptions(arch, product, osVersion, variant string) *osbuild.BuildstampStageOptions {
	return &osbuild.BuildstampStageOptions{
		Arch:    arch,
		Product: product,
		Version: osVersion,
		Variant: variant,
		Final:   true,
	}
}

func anacondaStageOptions() *osbuild.AnacondaStageOptions {
	return &osbuild.AnacondaStageOptions{
		KickstartModules: []string{
			"org.fedoraproject.Anaconda.Modules.Network",
			"org.fedoraproject.Anaconda.Modules.Payloads",
			"org.fedoraproject.Anaconda.Modules.Storage",
		},
	}
}

func loraxScriptStageOptions(arch string) *osbuild.LoraxScriptStageOptions {
	return &osbuild.LoraxScriptStageOptions{
		Path:     "99-generic/runtime-postinstall.tmpl",
		BaseArch: arch,
	}
}

func dracutStageOptions(kernelVer, arch string, additionalModules []string) *osbuild.DracutStageOptions {
	kernel := []string{kernelVer}
	modules := []string{
		"bash",
		"systemd",
		"fips",
		"systemd-initrd",
		"modsign",
		"nss-softokn",
		"rdma",
		"rngd",
		"i18n",
		"convertfs",
		"network-manager",
		"network",
		"ifcfg",
		"url-lib",
		"drm",
		"plymouth",
		"prefixdevname",
		"prefixdevname-tools",
		"crypt",
		"dm",
		"dmsquash-live",
		"kernel-modules",
		"kernel-modules-extra",
		"kernel-network-modules",
		"livenet",
		"lvm",
		"mdraid",
		"multipath",
		"qemu",
		"qemu-net",
		"fcoe",
		"fcoe-uefi",
		"iscsi",
		"lunmask",
		"nfs",
		"resume",
		"rootfs-block",
		"terminfo",
		"udev-rules",
		"dracut-systemd",
		"pollcdrom",
		"usrmount",
		"base",
		"fs-lib",
		"img-lib",
		"shutdown",
		"uefi-lib",
	}

	if arch == distro.X86_64ArchName {
		modules = append(modules, "biosdevname")
	}

	modules = append(modules, additionalModules...)
	return &osbuild.DracutStageOptions{
		Kernel:  kernel,
		Modules: modules,
		Install: []string{"/.buildstamp"},
	}
}

func tarKickstartStageOptions(tarURL string) *osbuild.KickstartStageOptions {
	return &osbuild.KickstartStageOptions{
		Path: kspath,
		LiveIMG: &osbuild.LiveIMG{
			URL: tarURL,
		},
	}
}

func ostreeKickstartStageOptions(ostreeURL, ostreeRef string) *osbuild.KickstartStageOptions {
	return &osbuild.KickstartStageOptions{
		Path: kspath,
		OSTree: &osbuild.OSTreeOptions{
			OSName: "rhel",
			URL:    ostreeURL,
			Ref:    ostreeRef,
			GPG:    false,
		},
	}
}

func bootISOMonoStageOptions(kernelVer, arch, vendor, product, osVersion, isolabel string) *osbuild.BootISOMonoStageOptions {
	comprOptions := new(osbuild.FSCompressionOptions)
	if bcj := osbuild.BCJOption(arch); bcj != "" {
		comprOptions.BCJ = bcj
	}
	var architectures []string

	if arch == distro.X86_64ArchName {
		architectures = []string{"IA32", "X64"}
	} else if arch == distro.Aarch64ArchName {
		architectures = []string{"AA64"}
	} else {
		panic("unsupported architecture")
	}

	return &osbuild.BootISOMonoStageOptions{
		Product: osbuild.Product{
			Name:    product,
			Version: osVersion,
		},
		ISOLabel:   isolabel,
		Kernel:     kernelVer,
		KernelOpts: fmt.Sprintf("inst.ks=hd:LABEL=%s:%s", isolabel, kspath),
		EFI: osbuild.EFI{
			Architectures: architectures,
			Vendor:        vendor,
		},
		ISOLinux: osbuild.ISOLinux{
			Enabled: arch == distro.X86_64ArchName,
			Debug:   false,
		},
		Templates: "80-rhel",
		RootFS: osbuild.RootFS{
			Size: 9216,
			Compression: osbuild.FSCompression{
				Method:  "xz",
				Options: comprOptions,
			},
		},
	}
}

func grubISOStageOptions(installDevice, kernelVer, arch, vendor, product, osVersion, isolabel string) *osbuild.GrubISOStageOptions {
	var architectures []string

	if arch == "x86_64" {
		architectures = []string{"IA32", "X64"}
	} else if arch == "aarch64" {
		architectures = []string{"AA64"}
	} else {
		panic("unsupported architecture")
	}

	return &osbuild.GrubISOStageOptions{
		Product: osbuild.Product{
			Name:    product,
			Version: osVersion,
		},
		ISOLabel: isolabel,
		Kernel: osbuild.ISOKernel{
			Dir: "/images/pxeboot",
			Opts: []string{"rd.neednet=1",
				"console=tty0",
				"console=ttyS0",
				"systemd.log_target=console",
				"systemd.journald.forward_to_console=1",
				"edge.liveiso=" + isolabel,
				"coreos.inst.install_dev=" + installDevice,
				"coreos.inst.image_file=/run/media/iso/disk.img.xz",
				"coreos.inst.insecure"},
		},
		Architectures: architectures,
		Vendor:        vendor,
	}
}

func discinfoStageOptions(arch string) *osbuild.DiscinfoStageOptions {
	return &osbuild.DiscinfoStageOptions{
		BaseArch: arch,
		Release:  "202010217.n.0",
	}
}

func xorrisofsStageOptions(filename, isolabel, arch string, isolinux bool) *osbuild.XorrisofsStageOptions {
	options := &osbuild.XorrisofsStageOptions{
		Filename: filename,
		VolID:    fmt.Sprintf(isolabel, arch),
		SysID:    "LINUX",
		EFI:      "images/efiboot.img",
	}

	if isolinux {
		options.Boot = &osbuild.XorrisofsBoot{
			Image:   "isolinux/isolinux.bin",
			Catalog: "isolinux/boot.cat",
		}

		options.IsohybridMBR = "/usr/share/syslinux/isohdpfx.bin"
	}

	return options
}

func grub2StageOptions(rootPartition *disk.Partition,
	bootPartition *disk.Partition,
	kernelOptions string,
	kernel *blueprint.KernelCustomization,
	kernelVer string,
	uefi bool,
	legacy string,
	vendor string,
	install bool) *osbuild.GRUB2StageOptions {
	if rootPartition == nil {
		panic("root partition must be defined for grub2 stage, this is a programming error")
	}

	stageOptions := osbuild.GRUB2StageOptions{
		RootFilesystemUUID: uuid.MustParse(rootPartition.Filesystem.UUID),
		KernelOptions:      kernelOptions,
		Legacy:             legacy,
	}

	if bootPartition != nil {
		bootFsUUID := uuid.MustParse(bootPartition.Filesystem.UUID)
		stageOptions.BootFilesystemUUID = &bootFsUUID
	}

	if uefi {
		stageOptions.UEFI = &osbuild.GRUB2UEFI{
			Vendor:  vendor,
			Install: install,
		}
	}

	if !uefi {
		stageOptions.Legacy = legacy
	}

	if kernel != nil {
		if kernel.Append != "" {
			stageOptions.KernelOptions += " " + kernel.Append
		}
		stageOptions.SavedEntry = "ffffffffffffffffffffffffffffffff-" + kernelVer
	}

	return &stageOptions
}

// sfdiskStageOptions creates the options and devices properties for an
// org.osbuild.sfdisk stage based on a partition table description
func sfdiskStageOptions(pt *disk.PartitionTable) *osbuild.SfdiskStageOptions {
	partitions := make([]osbuild.Partition, len(pt.Partitions))
	for idx, p := range pt.Partitions {
		partitions[idx] = osbuild.Partition{
			Bootable: p.Bootable,
			Size:     p.Size,
			Start:    p.Start,
			Type:     p.Type,
			UUID:     p.UUID,
		}
	}
	stageOptions := &osbuild.SfdiskStageOptions{
		Label:      pt.Type,
		UUID:       pt.UUID,
		Partitions: partitions,
	}

	return stageOptions
}

// copyFSTreeOptions creates the options, inputs, devices, and mounts properties
// for an org.osbuild.copy stage for a given source tree using a partition
// table description to define the mounts
func copyFSTreeOptions(inputName, inputPipeline string, pt *disk.PartitionTable, device *osbuild.Device) (
	*osbuild.CopyStageOptions,
	*osbuild.Devices,
	*osbuild.Mounts,
) {
	// assume loopback device for simplicity since it's the only one currently supported
	// panic if the conversion fails
	devOptions, ok := device.Options.(*osbuild.LoopbackDeviceOptions)
	if !ok {
		panic("copyStageOptions: failed to convert device options to loopback options")
	}

	devices := make(map[string]osbuild.Device, len(pt.Partitions))
	mounts := make([]osbuild.Mount, 0, len(pt.Partitions))
	for _, p := range pt.Partitions {
		if p.Filesystem == nil {
			// no filesystem for partition (e.g., BIOS boot)
			continue
		}
		name := filepath.Base(p.Filesystem.Mountpoint)
		if name == "/" {
			name = "root"
		}
		devices[name] = *osbuild.NewLoopbackDevice(
			&osbuild.LoopbackDeviceOptions{
				Filename: devOptions.Filename,
				Start:    p.Start,
				Size:     p.Size,
			},
		)
		var mount *osbuild.Mount
		switch p.Filesystem.Type {
		case "xfs":
			mount = osbuild.NewXfsMount(name, name, p.Filesystem.Mountpoint)
		case "vfat":
			mount = osbuild.NewFATMount(name, name, p.Filesystem.Mountpoint)
		case "ext4":
			mount = osbuild.NewExt4Mount(name, name, p.Filesystem.Mountpoint)
		case "btrfs":
			mount = osbuild.NewBtrfsMount(name, name, p.Filesystem.Mountpoint)
		default:
			panic("unknown fs type " + p.Type)
		}
		mounts = append(mounts, *mount)
	}

	// sort the mounts, using < should just work because:
	// - a parent directory should be always before its children:
	//   / < /boot
	// - the order of siblings doesn't matter
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].Target < mounts[j].Target
	})

	stageMounts := osbuild.Mounts(mounts)
	stageDevices := osbuild.Devices(devices)

	options := osbuild.CopyStageOptions{
		Paths: []osbuild.CopyStagePath{
			{
				From: fmt.Sprintf("input://%s/", inputName),
				To:   "mount://root/",
			},
		},
	}

	return &options, &stageDevices, &stageMounts
}

func grub2InstStageOptions(filename string, pt *disk.PartitionTable, platform string) *osbuild.Grub2InstStageOptions {
	bootPartIndex := pt.BootPartitionIndex()
	if bootPartIndex == -1 {
		panic("failed to find boot or root partition for grub2.inst stage")
	}
	bootPart := pt.Partitions[bootPartIndex]
	prefixPath := "/boot/grub2"
	if bootPart.Filesystem.Mountpoint == "/boot" {
		prefixPath = "/grub2"
	}
	core := osbuild.CoreMkImage{
		Type:       "mkimage",
		PartLabel:  pt.Type,
		Filesystem: pt.Partitions[bootPartIndex].Filesystem.Type,
	}

	prefix := osbuild.PrefixPartition{
		Type:      "partition",
		PartLabel: pt.Type,
		Number:    uint(bootPartIndex),
		Path:      prefixPath,
	}

	return &osbuild.Grub2InstStageOptions{
		Filename: filename,
		Platform: platform,
		Location: pt.Partitions[0].Start,
		Core:     core,
		Prefix:   prefix,
	}
}

func ziplInstStageOptions(kernel string, pt *disk.PartitionTable) *osbuild.ZiplInstStageOptions {
	bootPartIndex := pt.BootPartitionIndex()
	if bootPartIndex == -1 {
		panic("failed to find boot or root partition for zipl.inst stage")
	}

	return &osbuild.ZiplInstStageOptions{
		Kernel:   kernel,
		Location: pt.Partitions[bootPartIndex].Start,
	}
}

func qemuStageOptions(filename, format, compat string) *osbuild.QEMUStageOptions {
	var options osbuild.QEMUFormatOptions
	switch format {
	case "qcow2":
		options = osbuild.Qcow2Options{
			Type:   "qcow2",
			Compat: compat,
		}
	case "vpc":
		options = osbuild.VPCOptions{
			Type: "vpc",
		}
	case "vmdk":
		options = osbuild.VMDKOptions{
			Type: "vmdk",
		}
	default:
		panic("unknown format in qemu stage: " + format)
	}

	return &osbuild.QEMUStageOptions{
		Filename: filename,
		Format:   options,
	}
}

func kernelCmdlineStageOptions(rootUUID string, kernelOptions string) *osbuild.KernelCmdlineStageOptions {
	return &osbuild.KernelCmdlineStageOptions{
		RootFsUUID: rootUUID,
		KernelOpts: kernelOptions,
	}
}

func nginxConfigStageOptions(path, htmlRoot, listen string) *osbuild.NginxConfigStageOptions {
	// configure nginx to work in an unprivileged container
	cfg := &osbuild.NginxConfig{
		Listen: listen,
		Root:   htmlRoot,
		Daemon: common.BoolToPtr(false),
		PID:    "/tmp/nginx.pid",
	}
	return &osbuild.NginxConfigStageOptions{
		Path:   path,
		Config: cfg,
	}
}

func chmodStageOptions(path, mode string, recursive bool) *osbuild.ChmodStageOptions {
	return &osbuild.ChmodStageOptions{
		Items: map[string]osbuild.ChmodStagePathOptions{
			path: {Mode: mode, Recursive: recursive},
		},
	}
}

func ostreeConfigStageOptions(repo string, readOnly bool) *osbuild.OSTreeConfigStageOptions {
	return &osbuild.OSTreeConfigStageOptions{
		Repo: repo,
		Config: &osbuild.OSTreeConfig{
			Sysroot: &osbuild.SysrootOptions{
				ReadOnly:   common.BoolToPtr(readOnly),
				Bootloader: "none",
			},
		},
	}
}

func efiMkdirStageOptions() *osbuild.MkdirStageOptions {
	return &osbuild.MkdirStageOptions{
		Paths: []osbuild.Path{
			{
				Path: "/boot/efi",
				Mode: os.FileMode(0700),
			},
		},
	}
}
