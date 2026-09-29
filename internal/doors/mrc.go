package doors

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MRCConfig is uMRC's mrc.cfg: which Multi-Relay Chat host its
// umrc-bridge connects to, and what the chat network shows about this
// board. uMRC's own setup program asks for the same fields in a
// full-screen console; the web admin edits them instead.
type MRCConfig struct {
	Host string `json:"host"`
	Port string `json:"port"`
	SSL  bool   `json:"ssl"`
	// BBSName, Software, Website, Telnet, SSH, Sysop and Description
	// are shown to the chat network; all but Software may use Mystic
	// pipe colour codes (|01 to |23).
	BBSName     string `json:"bbs_name"`
	Software    string `json:"software"`
	Website     string `json:"website"`
	Telnet      string `json:"telnet"`
	SSH         string `json:"ssh"`
	Sysop       string `json:"sysop"`
	Description string `json:"description"`
}

// MRCConfigFile is uMRC's config file, in the door's directory.
const MRCConfigFile = "mrc.cfg"

// mrc.cfg is uMRC's C struct settings written as-is: fixed-size,
// NUL-padded char arrays and a one-byte bool, no padding in between.
var mrcLayout = []int{80, 6, 1, 140, 140, 140, 140, 140, 140, 140}

func mrcSize() int {
	n := 0
	for _, l := range mrcLayout {
		n += l
	}
	return n
}

// DefaultMRCConfig is a new board's configuration: the main MRC host
// over SSL, as uMRC's setup suggests.
func DefaultMRCConfig(bbsName, sysop string) MRCConfig {
	return MRCConfig{
		Host:     "na-multi.relaychat.net",
		Port:     "5001",
		SSL:      true,
		BBSName:  bbsName,
		Software: "NullModem BBS",
		Sysop:    sysop,
	}
}

func (c MRCConfig) fields() []string {
	return []string{c.Host, c.Port, "", c.BBSName, c.Software, c.Website, c.Telnet, c.SSH, c.Sysop, c.Description}
}

// Validate reports a field too long for mrc.cfg, or a missing host.
func (c MRCConfig) Validate() error {
	if c.Host == "" || c.Port == "" {
		return errors.New("the MRC host and port must be set")
	}
	names := []string{"host", "port", "", "BBS name", "software", "website", "telnet address", "SSH address", "sysop", "description"}
	for i, v := range c.fields() {
		if len(v) >= mrcLayout[i] && mrcLayout[i] > 1 {
			return fmt.Errorf("%s is too long (at most %d characters)", names[i], mrcLayout[i]-1)
		}
	}
	return nil
}

// ReadMRCConfig reads dir/mrc.cfg.
func ReadMRCConfig(dir string) (MRCConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, MRCConfigFile))
	if err != nil {
		return MRCConfig{}, fmt.Errorf("doors: reading %s: %w", MRCConfigFile, err)
	}
	if len(data) != mrcSize() {
		return MRCConfig{}, fmt.Errorf("doors: %s is %d bytes, not %d", MRCConfigFile, len(data), mrcSize())
	}
	var v []string
	off := 0
	for _, l := range mrcLayout {
		field := data[off : off+l]
		if i := bytes.IndexByte(field, 0); i >= 0 {
			field = field[:i]
		}
		v = append(v, string(field))
		off += l
	}
	return MRCConfig{
		Host: v[0], Port: v[1], SSL: data[86] != 0,
		BBSName: v[3], Software: v[4], Website: v[5], Telnet: v[6], SSH: v[7], Sysop: v[8], Description: v[9],
	}, nil
}

// WriteMRCConfig writes c to dir/mrc.cfg.
func WriteMRCConfig(dir string, c MRCConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	buf := make([]byte, 0, mrcSize())
	for i, v := range c.fields() {
		field := make([]byte, mrcLayout[i])
		copy(field, v)
		if i == 2 && c.SSL {
			field[0] = 1
		}
		buf = append(buf, field...)
	}
	if err := os.WriteFile(filepath.Join(dir, MRCConfigFile), buf, 0o644); err != nil {
		return fmt.Errorf("doors: writing %s: %w", MRCConfigFile, err)
	}
	return nil
}
