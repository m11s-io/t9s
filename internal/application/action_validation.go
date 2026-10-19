package application

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	"github.com/m11s-io/t9s/internal/domain"
)

// BuildResetPreview classifies a node's disks into system and writable user disks.
func BuildResetPreview(node string, set domain.DiskSet) ResetPreview {
	preview := ResetPreview{Node: node}
	if len(set.Disks) == 0 {
		return preview
	}
	preview.Known = true
	for _, disk := range set.Disks {
		if disk.SystemDisk {
			if preview.SystemDisk == "" {
				preview.SystemDisk = disk.DeviceName
			}
			continue
		}
		if !disk.ReadOnly {
			preview.UserDisks = append(preview.UserDisks, disk.DeviceName)
		}
	}
	return preview
}

// ResetConfirmationToken returns the typed intent required for a reset target set.
func ResetConfirmationToken(targets []string) string {
	switch len(targets) {
	case 0:
		return ""
	case 1:
		return targets[0]
	default:
		return fmt.Sprintf("wipe %d nodes", len(targets))
	}
}

// NormalizeDeviceToken trims whitespace and an optional /dev/ prefix.
func NormalizeDeviceToken(device string) string {
	return strings.TrimPrefix(strings.TrimSpace(device), "/dev/")
}

// ValidateDeviceWipeConfirmation checks that typed intent names the selected device.
func ValidateDeviceWipeConfirmation(device, typed string) error {
	if NormalizeDeviceToken(device) == "" {
		return fmt.Errorf("no device selected")
	}
	if NormalizeDeviceToken(typed) != NormalizeDeviceToken(device) {
		return fmt.Errorf("type %q to confirm wiping this device", device)
	}
	return nil
}

// ValidateEtcdSnapshotPath checks that path names a file accepted by the snapshot writer.
func ValidateEtcdSnapshotPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("snapshot path is required")
	}
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(filepath.Separator)) {
		return fmt.Errorf("snapshot path %q must name a file, not a directory", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." || strings.HasSuffix(cleaned, string(filepath.Separator)) {
		return fmt.Errorf("snapshot path %q must name a file", path)
	}
	switch filepath.Base(cleaned) {
	case "", ".", "..", ".part":
		return fmt.Errorf("snapshot path %q has an invalid file name", path)
	}
	return nil
}

// DefaultEtcdSnapshotPath returns a sanitized process-local filename for a snapshot.
func DefaultEtcdSnapshotPath(contextName, memberHostname string, now time.Time) string {
	return fmt.Sprintf("etcd-%s-%s-%s.db", sanitizeSnapshotToken(contextName), sanitizeSnapshotToken(memberHostname), now.UTC().Format("20060102T150405Z"))
}

func sanitizeSnapshotToken(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	token := builder.String()
	for strings.Contains(token, "..") {
		token = strings.ReplaceAll(token, "..", ".")
	}
	if token == "" {
		return "-"
	}
	return token
}

// UpgradeMinorWarning warns when an image skips one or more Talos minor releases.
func UpgradeMinorWarning(running, image string) string {
	run, err := semver.Parse(strings.TrimPrefix(running, "v"))
	if err != nil {
		return ""
	}
	colon := strings.LastIndex(image, ":")
	if colon < 0 {
		return ""
	}
	target, err := semver.Parse(strings.TrimPrefix(image[colon+1:], "v"))
	if err != nil {
		return ""
	}
	if target.Major == run.Major && target.Minor > run.Minor+1 {
		return "skips intermediate Talos minor releases"
	}
	return ""
}
