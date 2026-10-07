// Package config loads the BBS daemon's YAML configuration.
package config

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds settings for cmd/bbs.
type Config struct {
	BBS struct {
		Name  string `yaml:"name"`
		Sysop string `yaml:"sysop"`
		// Location is where the board is ("Neunkirch, Switzerland"),
		// sent to BinkP peers in the handshake's LOC line.
		Location  string `yaml:"location,omitempty"`
		NewUserSL int    `yaml:"new_user_sl"`
		// PublicFeeds offers an RSS feed of every area a new caller may
		// read, without login (the front page lists them).
		PublicFeeds bool `yaml:"public_feeds,omitempty"`
		// MonthlyRecapOff stops the monthly recap netmail to the sysops.
		MonthlyRecapOff bool `yaml:"monthly_recap_off,omitempty"`
		// ChatAnnounceSysops says in the chat rooms when a sysop enters
		// or leaves; off, they come and go without a word.
		ChatAnnounceSysops bool `yaml:"chat_announce_sysops,omitempty"`
		// Language is the board's language (an internal/i18n code):
		// what callers read before they log in, and after if they never
		// chose one. "" is English.
		Language string `yaml:"language,omitempty"`
		// LangDir holds the sysop's changes to the texts (the language
		// editor); "" is data/lang.
		LangDir    string `yaml:"lang_dir,omitempty"`
		MenusDir   string `yaml:"menus_dir"`
		ScreensDir string `yaml:"screens_dir"`
		FilesDir   string `yaml:"files_dir"`
		// DoorsDir is where doors installed from the web admin's
		// templates go, one directory each.
		DoorsDir string `yaml:"doors_dir"`
		// FTNAddresses are this system's own FTN addresses (AKAs) on
		// whichever FTN-compatible network(s) it belongs to (FidoNet,
		// fsxNet, etc.) -- most systems have exactly one, but a point
		// reachable through the same uplink under more than one
		// network needs to identify with all of them in the same
		// BinkP session (see internal/tosser and internal/binkp's
		// OurAddresses). The first address is "primary": it's stamped
		// as the From address on locally composed netmail and shown
		// as this system's main address in the web UI. Optional and
		// empty by default -- not every BBS is on an FTN network.
		FTNAddresses []string `yaml:"ftn_addresses"`
	} `yaml:"bbs"`

	// Networks are the FTN networks this system belongs to, each a
	// short name (shown as the group of its areas, and what an uplink's
	// and an area's Network refer to) and its FTN domain -- see
	// Network.
	Networks []Network `yaml:"networks"`
	// NetworkRenames is filled by Load when it derived Networks from an
	// older config (see migrateLegacyNetworks): old group name -> new
	// short name, for renaming the areas in the database to match. Not
	// part of the file.
	NetworkRenames map[string]string `yaml:"-"`

	// Doors are external door programs callers can launch from the
	// BBS menu (see internal/doors) -- each one an already-installed
	// executable on this host, not managed by this project itself.
	Doors []DoorConfig `yaml:"doors"`
	// SecurityLevels names the security levels the sysop uses ("20 --
	// Regular user"), shown wherever one is chosen. Empty: the levels
	// the board itself knows (see Levels).
	SecurityLevels []SecurityLevel `yaml:"security_levels,omitempty"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`

	Telnet struct {
		Enabled bool   `yaml:"enabled"`
		Addr    string `yaml:"addr"`
	} `yaml:"telnet"`

	SSH struct {
		Enabled     bool   `yaml:"enabled"`
		Addr        string `yaml:"addr"`
		HostKeyPath string `yaml:"host_key_path"`
	} `yaml:"ssh"`

	Binkp struct {
		// Uplinks are the BinkP nodes/hubs this system polls to
		// exchange netmail (see internal/tosser). Echomail routing
		// isn't implemented yet -- only netmail is tossed so far.
		Uplinks []BinkpUplink `yaml:"uplinks"`
		// PollIntervalSeconds is the default interval cmd/mailer waits
		// between polls of an uplink that doesn't set its own
		// BinkpUplink.PollIntervalSeconds. The web admin's "Send Now"
		// button (see internal/web) polls on demand regardless of
		// this interval.
		PollIntervalSeconds int `yaml:"poll_interval_seconds"`
		// ListenEnabled and ListenAddr configure cmd/mailer's inbound
		// BinkP listener (a caller dialing us, e.g. a hub that wants to
		// push mail between our own scheduled polls rather than
		// waiting for us to ask -- see tosser.Answer). A caller is
		// authenticated by matching its claimed FTN address against
		// Uplinks, so only a system already configured as one of our
		// uplinks can push mail to us; nothing else is accepted.
		ListenEnabled bool   `yaml:"listen_enabled"`
		ListenAddr    string `yaml:"listen_addr"`
	} `yaml:"binkp"`
	// Maintenance is the nightly cleanup (see internal/maintenance).
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	// Backup is the nightly backup (see internal/backup).
	Backup BackupConfig `yaml:"backup"`
	// Discord bridges chat rooms to Discord channels (internal/discord;
	// which room goes where is set per room in the web admin).
	Discord DiscordConfig `yaml:"discord,omitempty"`
	// Matrix bridges chat rooms to Matrix rooms (internal/matrix).
	Matrix MatrixConfig `yaml:"matrix,omitempty"`
	// Email is the netmail <-> email gateway (internal/emailgw).
	Email EmailConfig `yaml:"email,omitempty"`
	// Security is the login protection and new-user approval (see
	// internal/guard).
	Security SecurityConfig `yaml:"security"`
	// InterBBS is taking part in inter-BBS lists carried in data echoes.
	InterBBS struct {
		LastCallers LastCallersConfig `yaml:"last_callers"`
	} `yaml:"interbbs"`
}

// BinkpUplink is one BinkP node/hub this system connects out to.
type BinkpUplink struct {
	// Address is the uplink's own FTN address, for display/reference
	// only -- it's not cross-checked against what the uplink actually
	// claims via M_ADR when connecting.
	Address string `yaml:"address"`
	// Host is "host:port", e.g. "bbs.example.com:24554".
	Host string `yaml:"host"`
	// Password authenticates the BinkP *session* itself (sent as a
	// CRAM-MD5 response if the uplink advertises support, otherwise
	// in the clear -- see internal/binkp.Config.Password), distinct
	// from PacketPassword below. Empty means no session password is
	// sent.
	Password string `yaml:"password"`
	// PollDisabled excludes this uplink from cmd/mailer's regular,
	// interval-based scheduled poll -- for a link that should only
	// ever be dialed when there's actually something to send (any
	// pending netmail or echomail routed to it -- see cmd/mailer's
	// crash-style triggering, not just Crash-flagged netmail
	// specifically), plus its own PollIntervalSeconds as a slow
	// fallback if set (needed for a link with no way to push mail
	// to us the way internal/tosser.Answer lets a configured
	// uplink do -- otherwise it would only ever receive anything
	// when Crash-flagged netmail happened to be routed to it), or
	// purely on demand via the web admin's "Send Now" button
	// (which ignores this flag entirely).
	PollDisabled bool `yaml:"poll_disabled"`
	// PollIntervalSeconds overrides Binkp.PollIntervalSeconds for
	// this uplink specifically -- some hubs only permit polling every
	// hour or two and reject a caller that connects more often (see
	// internal/tosser's IsDue). Zero means "use the global default".
	// Meaningless when PollDisabled is set.
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	// PacketPassword authenticates the FTS-0001 packet itself (the
	// 8-byte password field in a .pkt file's own header -- see
	// internal/mail.PacketHeader), independent of the BinkP session
	// Password above. internal/tosser stamps it on every outbound
	// packet built for this uplink, and rejects an inbound packet
	// whose own header password doesn't match. Empty means no packet
	// password is set or expected.
	PacketPassword string `yaml:"packet_password"`
	// TICPassword authenticates TIC file-echo announcements from this
	// uplink (the "type 2" file-distribution protocol layered over
	// BinkP, see internal/tic and internal/tosser's ticSession):
	// accepted the same way PacketPassword is -- any password
	// configured for this uplink's own host, not just this specific
	// entry's (see acceptedTICPasswords). Empty means no password is
	// expected/checked for file-echo from this uplink.
	TICPassword string `yaml:"tic_password"`
	// AreafixPassword authenticates automated echomail area
	// subscription requests to this uplink's "Areafix" netmail robot
	// (see internal/tosser's RequestEchoAreaSubscription) -- the first
	// line of every request's body. Empty sends an empty password
	// line, which a hub requiring one will reject.
	AreafixPassword string `yaml:"areafix_password"`
	// FilefixPassword is AreafixPassword's exact counterpart for
	// file-echo (TIC) area subscription requests to this uplink's
	// "Filefix" robot (see internal/tosser's
	// RequestFileAreaSubscription) -- a real hub commonly runs Filefix
	// as a distinct robot from Areafix with its own password, so this
	// is deliberately not just AreafixPassword reused.
	FilefixPassword string `yaml:"filefix_password"`
	// Network labels which FTN network this uplink carries echomail
	// for (e.g. "fsxNet", "HobbyNet"), matched case-insensitively
	// against a message area's own Network field (set by the sysop
	// when approving/editing an area -- see internal/message.Area) to
	// decide which uplink a locally-posted echo message goes out
	// through. Empty means this uplink never carries any locally-
	// originated echomail (fine for a network we only read, never
	// post to).
	Network string `yaml:"network"`
	// Hold marks this uplink as classic FTN "Hold" status: cmd/mailer
	// never dials it automatically for any reason at all, not even
	// PollDisabled's own "crash-style" immediate dial for pending mail
	// (see cmd/mailer's checkUplinks/dialedForPendingMail) -- mail
	// just queues until the uplink itself polls us (see
	// Binkp.ListenEnabled) or a sysop uses the web admin's "Send Now"
	// button, which ignores Hold entirely, same as it already ignores
	// PollDisabled. For a genuinely unreachable peer (e.g. a point
	// behind NAT with no port forwarding) PollDisabled alone isn't
	// enough: pending mail -- Crash-flagged or not -- would still
	// trigger a doomed dial attempt on every check tick.
	Hold bool `yaml:"hold"`
	// NoCRAM sends Password in the clear even when the uplink offers
	// CRAM-MD5 (see internal/binkp.Config.NoCRAM). Off by default; only
	// for testing whether a hub's trouble is tied to CRAM logins.
	NoCRAM bool `yaml:"no_cram"`
	// AKAAddresses restricts which of this system's own FTN
	// addresses/AKAs (Config.BBS.FTNAddresses) belong to this uplink
	// specifically: only these are presented via BinkP's M_ADR when
	// internal/tosser.Poll dials it, and only these addresses' own
	// zones are considered this uplink's for Crash-mail routing (see
	// internal/tosser's routeOutbound/uplinkForDestination),
	// replacing the automatic "shares Address's own zone" default.
	// Needed because presenting every configured AKA to every uplink
	// leaks addresses that have nothing to do with a given hub --
	// confirmed live against a real hub, whose own software
	// auto-registers a new node entry for every M_ADR address it
	// sees, aliases included. Empty means the old automatic behavior:
	// every configured address is presented to every uplink, and only
	// Address's own zone is used for Crash routing.
	AKAAddresses []string `yaml:"aka_addresses,omitempty"`
	// Downlink is true for one of this system's own points/nodes it
	// feeds (shown under "Nodes / Points" in the uplinks page), false
	// for an upstream hub/network feed this system itself depends on
	// (shown under "Hubs", the default). For a node that's all it
	// does. A downlink with a point address (21:3/194.1) is a point of
	// this system, which internal/tosser treats as such: it only gets
	// netmail addressed to it and the areas it subscribed to, never
	// this system's own outgoing mail (see internal/tosser's
	// points.go).
	Downlink bool `yaml:"downlink,omitempty"`
}

// DoorConfig is one entry in Config.Doors -- see internal/doors.Door,
// which this maps directly onto (kept as a separate type, like
// BinkpUplink, so config stays decoupled from other internal
// packages' own types).
type DoorConfig struct {
	// Name identifies this door in the in-BBS doors menu and in logs.
	Name string `yaml:"name"`
	// Kind selects how the door is launched: "native" (default, the
	// zero value) or "dosbox" -- see internal/doors.Door.Kind.
	Kind string `yaml:"kind"`
	// MinSL is the minimum security level required to play.
	MinSL int `yaml:"min_sl"`

	// Exe is the path to the door's executable. Kind "native" only.
	Exe string `yaml:"exe"`
	// Dir is the working directory to run Exe from -- almost always
	// the door's own install directory. Kind "native" only.
	Dir string `yaml:"dir"`
	// Args are extra arguments passed before the dropfile path
	// argument internal/doors.Run appends itself. Kind "native" only.
	Args []string `yaml:"args"`

	// DOSBoxDir is the door's own install directory, mounted as C: in
	// the DOSBox-X guest. Kind "dosbox" only.
	DOSBoxDir string `yaml:"dosbox_dir"`
	// DOSBoxLaunchCmd is the DOS command line that starts the door --
	// see internal/doors.Door.DOSBoxLaunchCmd. Kind "dosbox" only.
	DOSBoxLaunchCmd string `yaml:"dosbox_launch_cmd"`

	// DropFile is the drop file format ("door.sys", "dorinfo",
	// "doorfile.sr", "door32.sys"); empty means the kind's default --
	// see internal/doors.Door.DropFile.
	DropFile string `yaml:"dropfile,omitempty"`
	// DropFileInDoorDir also writes the drop file into the door's own
	// directory -- see internal/doors.Door.DropFileInDoorDir.
	DropFileInDoorDir bool `yaml:"dropfile_in_door_dir,omitempty"`
	// LockFiles are cleared before the door starts when nobody else is
	// playing it -- see internal/doors.Door.LockFiles.
	LockFiles []string `yaml:"lock_files,omitempty"`
	// Stdio runs a native door over standard I/O -- see
	// internal/doors.Door.Stdio.
	Stdio bool `yaml:"stdio,omitempty"`
	// ANSI16 reduces the door's colours to the 16 classic ones -- see
	// internal/doors.Door.ANSI16.
	ANSI16 bool `yaml:"ansi16,omitempty"`
	// Daily is the door's daily maintenance (new turns, events): DOS
	// commands for a "dosbox" door (one per line, run from C:), a
	// command line for a native one (relative to Dir); run headless
	// once a day at DailyAt ("HH:MM", default "00:05"). Empty: none.
	Daily   string `yaml:"daily,omitempty"`
	DailyAt string `yaml:"daily_at,omitempty"`
	// Remote is where a door of kind "rlogin" is played: a door
	// server (DoorParty, a friend's BBS) reached over RLogin. The BBS
	// connects, names the caller (RemoteUser/RemotePassword, see
	// internal/bbs's rlogin.go) and passes everything through.
	Remote RemoteDoor `yaml:"remote,omitempty"`

	// Bulletins are files the door writes for the board -- its
	// scoreboard, its news -- shown in the doors menu and the portal;
	// Public ones also on the front page.
	Bulletins []DoorBulletin `yaml:"bulletins,omitempty"`

	// Template names the door template (internal/doors.Templates) this
	// entry was created from, if any -- informational, shown in the
	// web admin.
	Template string `yaml:"template,omitempty"`
	// Program is a background program the door needs running all the
	// time (uMRC's umrc-bridge), started in the door's directory -- see
	// internal/doors.Supervisor. Native doors only.
	Program []string `yaml:"program,omitempty"`
}

// PrimaryFTNAddress returns c's first configured FTN address, or "" if
// none are configured -- the address stamped on locally composed
// netmail and shown as this system's main address, for callers that
// only care about the single "our own address" case rather than the
// full AKA list a BinkP session presents.
func (c *Config) PrimaryFTNAddress() string {
	if len(c.BBS.FTNAddresses) == 0 {
		return ""
	}
	return c.BBS.FTNAddresses[0]
}

// TextsDir is where the sysop's changes to the texts are kept.
func (c *Config) TextsDir() string {
	if c.BBS.LangDir != "" {
		return c.BBS.LangDir
	}
	return "data/lang"
}

// Default returns the built-in configuration used when no config file
// is present, so the BBS is runnable out of the box.
func Default() *Config {
	c := &Config{}
	c.BBS.Name = "NullModem BBS"
	c.BBS.Sysop = "sysop"
	c.BBS.NewUserSL = 10
	c.BBS.MenusDir = "configs/menus"
	c.BBS.ScreensDir = "configs/screens"
	c.BBS.FilesDir = "data/files"
	c.BBS.DoorsDir = "data/doors"
	c.Database.Path = "data/nullmodem.sqlite"
	c.Telnet.Enabled = true
	c.Telnet.Addr = ":2323"
	c.SSH.Enabled = true
	c.SSH.Addr = ":2222"
	c.SSH.HostKeyPath = "data/ssh_host_key"
	c.Binkp.PollIntervalSeconds = 900
	return c
}

// Load reads and parses a YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	c := Default()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	migrateLegacyFTNAddress(data, c)
	migrateLegacyNetworks(c)
	return c, nil
}

// migrateLegacyFTNAddress folds an already-deployed config's old
// singular "bbs.ftn_address" key into FTNAddresses if that list came
// back empty, so upgrading to the list form (see FTNAddresses' doc
// comment) doesn't silently drop an address someone already
// configured. The next Save rewrites the file in the new shape,
// dropping the legacy key for good.
func migrateLegacyFTNAddress(data []byte, c *Config) {
	if len(c.BBS.FTNAddresses) > 0 {
		return
	}
	var legacy struct {
		BBS struct {
			FTNAddress string `yaml:"ftn_address"`
		} `yaml:"bbs"`
	}
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return
	}
	if legacy.BBS.FTNAddress != "" {
		c.BBS.FTNAddresses = []string{legacy.BBS.FTNAddress}
	}
}

// Save marshals c as YAML and writes it to path, overwriting any
// existing file. Used by the web admin API to persist edits made
// through the config UI.
func Save(path string, c *Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}

// LastCallersConfig is the InterBBS Last Callers list (see
// internal/lastcallers): the boards of a network post a record to a
// data echo whenever a caller logs off, and show who was on lately.
type LastCallersConfig struct {
	// Enabled posts a record for each caller logging off over
	// Telnet/SSH.
	Enabled bool `yaml:"enabled"`
	// Area is the data echo's tag; empty means FSX_DAT.
	Area string `yaml:"area,omitempty"`
	// Address is how callers reach this board, as shown on the other
	// boards' lists ("bbs.example.org:2323").
	Address string `yaml:"address,omitempty"`
	// System is the board's system as shown there; empty means Linux.
	System string `yaml:"system,omitempty"`
	// ShowAtLogin shows the list to a caller after logging in.
	ShowAtLogin bool `yaml:"show_at_login"`
}

// AreaTag is Area, or FSX_DAT.
func (c LastCallersConfig) AreaTag() string {
	if c.Area == "" {
		return "FSX_DAT"
	}
	return c.Area
}

// SystemName is System, or Linux.
func (c LastCallersConfig) SystemName() string {
	if c.System == "" {
		return "Linux"
	}
	return c.System
}

// MaintenanceConfig is the nightly cleanup (internal/maintenance). A
// value left out of the file takes its default (see the accessors); 0
// means "keep everything" wherever a limit is optional.
type MaintenanceConfig struct {
	// Enabled runs it every night in the mailer; off, it only runs
	// when asked in the web admin.
	Enabled bool `yaml:"enabled"`
	// Hour is when (0-23, the server's clock); default 4.
	Hour *int `yaml:"hour,omitempty"`
	// MessageKeepDays and MessageKeepMax limit every echomail area
	// (default 365 days, no count limit); an area can set its own.
	MessageKeepDays *int `yaml:"message_keep_days,omitempty"`
	MessageKeepMax  *int `yaml:"message_keep_max,omitempty"`
	// DataAreaKeepDays is the limit for data areas (message.Area.
	// Hidden: FSX_DAT) without one of their own; default 30.
	DataAreaKeepDays *int `yaml:"data_area_keep_days,omitempty"`
	// FileKeepDays limits file areas without a limit of their own;
	// default 0 (keep).
	FileKeepDays *int `yaml:"file_keep_days,omitempty"`
	// NetmailKeepDays deletes read netmail this old; default 0 (keep).
	NetmailKeepDays *int `yaml:"netmail_keep_days,omitempty"`
	// LogKeepRows is how many log entries are kept; default 5000.
	LogKeepRows *int `yaml:"log_keep_rows,omitempty"`
	// TranscriptKeepDays and ArchiveKeepDays keep BinkP session
	// transcripts and the inbound archive; default 5 days each.
	TranscriptKeepDays *int `yaml:"transcript_keep_days,omitempty"`
	ArchiveKeepDays    *int `yaml:"archive_keep_days,omitempty"`
	// Vacuum compacts the database afterwards; default on.
	Vacuum *bool `yaml:"vacuum,omitempty"`
	// PendingUserDays deletes accounts still waiting for approval
	// after this many days (bots that signed up); default 30, 0 keeps.
	PendingUserDays *int `yaml:"pending_user_days,omitempty"`
}

// BackupConfig is the nightly backup (internal/backup), written by the
// web daemon. On unless turned off.
type BackupConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	// Hour is when (0-23, the server's clock); default 3, before the
	// maintenance.
	Hour *int `yaml:"hour,omitempty"`
	// KeepDaily backups are kept (default 7), plus the newest of each
	// of the last KeepWeekly weeks (default 4).
	KeepDaily  *int `yaml:"keep_daily,omitempty"`
	KeepWeekly *int `yaml:"keep_weekly,omitempty"`
	// Dir is where backups go; default data/backups.
	Dir string `yaml:"dir,omitempty"`
	// IncludeFiles adds the file areas and doors (large).
	IncludeFiles bool `yaml:"include_files,omitempty"`
	// Offsite copies each backup, encrypted, to a storage service.
	Offsite OffsiteConfig `yaml:"offsite,omitempty"`
}

// OffsiteConfig is where encrypted copies of the backups go
// (internal/offsite): an SFTP server (a Hetzner Storage Box, say) or
// OpenStack Swift (Infomaniak Swiss Backup, say).
type OffsiteConfig struct {
	Enabled bool `yaml:"enabled"`
	// Kind is "sftp" or "swift".
	Kind string `yaml:"kind,omitempty"`
	// Recipient is the age public key ("age1...") copies are encrypted
	// to; only the sysop holds the private key.
	Recipient string `yaml:"recipient,omitempty"`
	// KeepDaily / KeepWeekly copies are kept there (default 14 / 8).
	KeepDaily  *int `yaml:"keep_daily,omitempty"`
	KeepWeekly *int `yaml:"keep_weekly,omitempty"`

	SFTP   OffsiteSFTP   `yaml:"sftp,omitempty"`
	Swift  OffsiteSwift  `yaml:"swift,omitempty"`
	S3     OffsiteS3     `yaml:"s3,omitempty"`
	WebDAV OffsiteWebDAV `yaml:"webdav,omitempty"`
}

// OffsiteS3 is an S3 bucket: AWS or any compatible service (Hetzner
// Object Storage, Infomaniak, Wasabi, Backblaze B2, Exoscale, MinIO).
// Endpoint is the service's host ("s3.amazonaws.com", "fsn1.your-
// objectstorage.com"); PathStyle for services that need path-style
// addressing (MinIO).
type OffsiteS3 struct {
	Endpoint  string `yaml:"endpoint,omitempty"`
	Region    string `yaml:"region,omitempty"`
	Bucket    string `yaml:"bucket,omitempty"`
	AccessKey string `yaml:"access_key,omitempty"`
	SecretKey string `yaml:"secret_key,omitempty"`
	Prefix    string `yaml:"prefix,omitempty"`
	PathStyle bool   `yaml:"path_style,omitempty"`
	// Insecure uses plain HTTP (a MinIO on the LAN, tests).
	Insecure bool `yaml:"insecure,omitempty"`
}

// OffsiteWebDAV is a WebDAV folder (Nextcloud, ownCloud, kDrive,
// a Storage Box's WebDAV): URL is the folder, with Basic auth.
type OffsiteWebDAV struct {
	URL      string `yaml:"url,omitempty"`
	User     string `yaml:"user,omitempty"`
	Password string `yaml:"password,omitempty"`
}

func (o OffsiteConfig) Daily() int  { return intOr(o.KeepDaily, 14) }
func (o OffsiteConfig) Weekly() int { return intOr(o.KeepWeekly, 8) }

// OffsiteSFTP is an SFTP server. Key is a private key the web admin
// made (its public half goes onto the server); HostKey pins the
// server's key (SHA256 fingerprint), learned on the first connect.
type OffsiteSFTP struct {
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	User     string `yaml:"user,omitempty"`
	Password string `yaml:"password,omitempty"`
	Key      string `yaml:"key,omitempty"`
	HostKey  string `yaml:"host_key,omitempty"`
	Dir      string `yaml:"dir,omitempty"`
}

// OffsiteSwift is an OpenStack Swift account, logged in through
// Keystone v3 (AuthURL ".../identity/v3").
type OffsiteSwift struct {
	AuthURL       string `yaml:"auth_url,omitempty"`
	User          string `yaml:"user,omitempty"`
	Password      string `yaml:"password,omitempty"`
	Project       string `yaml:"project,omitempty"`
	UserDomain    string `yaml:"user_domain,omitempty"`
	ProjectDomain string `yaml:"project_domain,omitempty"`
	Region        string `yaml:"region,omitempty"`
	Container     string `yaml:"container,omitempty"`
	// Prefix goes before the names ("bbs/"), "" for none.
	Prefix string `yaml:"prefix,omitempty"`
}

func (b BackupConfig) On() bool     { return b.Enabled == nil || *b.Enabled }
func (b BackupConfig) RunHour() int { return intOr(b.Hour, 3) }
func (b BackupConfig) Daily() int   { return intOr(b.KeepDaily, 7) }
func (b BackupConfig) Weekly() int  { return intOr(b.KeepWeekly, 4) }
func (b BackupConfig) Directory() string {
	if b.Dir == "" {
		return "data/backups"
	}
	return b.Dir
}

// SecurityConfig: locking out IPs that keep failing to log in
// (internal/guard), and new accounts waiting for the sysop's approval.
type SecurityConfig struct {
	// LockoutEnabled locks out an IP after MaxFailures failed logins
	// within WindowMinutes, for LockoutMinutes -- four times as long
	// each time it happens again within a day, up to MaxLockoutHours.
	// Default on: 5 in 10 minutes, 15 minutes, at most 24 hours.
	LockoutEnabled  *bool `yaml:"lockout_enabled,omitempty"`
	MaxFailures     *int  `yaml:"max_failures,omitempty"`
	WindowMinutes   *int  `yaml:"window_minutes,omitempty"`
	LockoutMinutes  *int  `yaml:"lockout_minutes,omitempty"`
	MaxLockoutHours *int  `yaml:"max_lockout_hours,omitempty"`
	// MaxConnectionsPerIP limits simultaneous Telnet/SSH connections
	// from one address; default 3, 0 no limit.
	MaxConnectionsPerIP *int `yaml:"max_connections_per_ip,omitempty"`
	// IdleMinutes hangs up on a logged-in caller who hasn't typed for
	// that long (default 30; 0: never). Before login it's always a few
	// minutes (internal/bbs.LoginIdleLimit).
	IdleMinutes *int `yaml:"idle_minutes,omitempty"`
	// ApproveNewUsers: new accounts start at PendingSL and wait for
	// the sysop's approval before they may post; approval raises them
	// to bbs.new_user_sl. Default on, PendingSL 5.
	ApproveNewUsers *bool `yaml:"approve_new_users,omitempty"`
	// RequireAdminTOTP: sysop accounts without two-factor login can't
	// get into the web admin or the Telnet sysop menu.
	RequireAdminTOTP bool `yaml:"require_admin_totp,omitempty"`
	PendingSL        *int `yaml:"pending_sl,omitempty"`
	// BlockedHandles can't be registered, in addition to the built-in
	// ones (sysop, admin, root, ...).
	BlockedHandles []string `yaml:"blocked_handles,omitempty"`
}

func (c SecurityConfig) Lockout() bool       { return c.LockoutEnabled == nil || *c.LockoutEnabled }
func (c SecurityConfig) Failures() int       { return intOr(c.MaxFailures, 5) }
func (c SecurityConfig) Window() int         { return intOr(c.WindowMinutes, 10) }
func (c SecurityConfig) LockoutMins() int    { return intOr(c.LockoutMinutes, 15) }
func (c SecurityConfig) MaxLockout() int     { return intOr(c.MaxLockoutHours, 24) }
func (c SecurityConfig) MaxConnections() int { return intOr(c.MaxConnectionsPerIP, 3) }
func (c SecurityConfig) Idle() int           { return intOr(c.IdleMinutes, 30) }
func (c SecurityConfig) Approval() bool      { return c.ApproveNewUsers == nil || *c.ApproveNewUsers }
func (c SecurityConfig) Pending() int        { return intOr(c.PendingSL, 5) }

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// The effective values, defaults filled in.
func (m MaintenanceConfig) RunHour() int        { return intOr(m.Hour, 4) }
func (m MaintenanceConfig) MessageDays() int    { return intOr(m.MessageKeepDays, 365) }
func (m MaintenanceConfig) MessageMax() int     { return intOr(m.MessageKeepMax, 0) }
func (m MaintenanceConfig) DataAreaDays() int   { return intOr(m.DataAreaKeepDays, 30) }
func (m MaintenanceConfig) FileDays() int       { return intOr(m.FileKeepDays, 0) }
func (m MaintenanceConfig) NetmailDays() int    { return intOr(m.NetmailKeepDays, 0) }
func (m MaintenanceConfig) LogRows() int        { return intOr(m.LogKeepRows, 5000) }
func (m MaintenanceConfig) TranscriptDays() int { return intOr(m.TranscriptKeepDays, 5) }
func (m MaintenanceConfig) ArchiveDays() int    { return intOr(m.ArchiveKeepDays, 5) }
func (m MaintenanceConfig) VacuumAfter() bool   { return m.Vacuum == nil || *m.Vacuum }
func (m MaintenanceConfig) PendingDays() int    { return intOr(m.PendingUserDays, 30) }

// Cached returns a loader for the config at path that reads it again
// at most every ttl -- for settings the web admin changes while a
// daemon runs (security, approval). fallback is used until a read
// succeeds, and the last good one after a failed read.
func Cached(path string, ttl time.Duration, fallback *Config) func() *Config {
	var mu sync.Mutex
	cur, at := fallback, time.Time{}
	return func() *Config {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) < ttl {
			return cur
		}
		at = time.Now()
		if c, err := Load(path); err == nil {
			cur = c
		}
		return cur
	}
}

// GuardSettings are the lockout limits in guard's terms (as plain
// values, so internal/guard needs no config import).
func (c SecurityConfig) GuardSettings() (enabled bool, maxFailures int, window, lockout, maxLockout time.Duration) {
	return c.Lockout(), c.Failures(), time.Duration(c.Window()) * time.Minute,
		time.Duration(c.LockoutMins()) * time.Minute, time.Duration(c.MaxLockout()) * time.Hour
}

// RemoteDoor is a door played on another system over RLogin
// (DoorConfig.Kind "rlogin").
type RemoteDoor struct {
	Host string `yaml:"host,omitempty" json:"host"`
	// Port defaults to 513.
	Port int `yaml:"port,omitempty" json:"port"`
	// ClientUser and ServerUser are RLogin's two user names, sent as
	// typed with the placeholders {handle}, {realname}, {node} and
	// {userid} filled in. Door networks say what they want there --
	// DoorParty: "[TAG]{handle}" and the system's password, for one.
	ClientUser string `yaml:"client_user,omitempty" json:"client_user"`
	ServerUser string `yaml:"server_user,omitempty" json:"server_user"`
	// TermType is sent as the terminal; default "ansi-bbs/115200".
	TermType string `yaml:"term_type,omitempty" json:"term_type"`
}

// DiscordConfig is the Discord bridge's bot.
type DiscordConfig struct {
	Enabled bool `yaml:"enabled"`
	// Token is the bot's token (Discord Developer Portal -> Bot).
	Token string `yaml:"token,omitempty"`
	// Quiet: don't tell Discord when someone enters or leaves a room.
	Quiet bool `yaml:"quiet,omitempty"`
}

// MatrixConfig is the Matrix bridge's bot account.
type MatrixConfig struct {
	Enabled bool `yaml:"enabled"`
	// Homeserver is the bot's server, e.g. https://matrix.org.
	Homeserver string `yaml:"homeserver,omitempty"`
	// UserID is the bot, e.g. @maiksplace:matrix.org; Token its access
	// token (the web admin logs in once and keeps only this).
	UserID string `yaml:"user_id,omitempty"`
	Token  string `yaml:"token,omitempty"`
	// Quiet: don't tell Matrix when someone enters or leaves a room.
	Quiet bool `yaml:"quiet,omitempty"`
}

// DoorBulletin is a file a door writes for the board: File relative to
// the door's directory (its DOSBox directory for a DOS door), ANSI or
// text.
type DoorBulletin struct {
	Title  string `yaml:"title" json:"title"`
	File   string `yaml:"file" json:"file"`
	Public bool   `yaml:"public,omitempty" json:"public"`
}

// EmailConfig is the netmail <-> email gateway: callers write to and
// get mail from handle@Domain, through a (catch-all) mailbox fetched
// over IMAP and an SMTP server to send.
type EmailConfig struct {
	Enabled bool `yaml:"enabled"`
	// Domain is the callers' mail domain: SwissMaik is
	// swissmaik@Domain.
	Domain string `yaml:"domain,omitempty"`
	// IMAP is the mailbox the domain's mail arrives in (a catch-all).
	IMAP MailServer `yaml:"imap,omitempty"`
	// SMTP sends the callers' mail.
	SMTP MailServer `yaml:"smtp,omitempty"`
	// MinSL is the security level from which (approved) callers may use
	// the gateway; mail to anyone else is dropped.
	MinSL int `yaml:"min_sl,omitempty"`
	// DailyLimit is how many mails a caller may send per day; nil: 20.
	DailyLimit *int `yaml:"daily_limit,omitempty"`
	// DeleteFetched deletes a mail from the mailbox once it's in the
	// BBS; otherwise it's only marked read.
	DeleteFetched bool `yaml:"delete_fetched,omitempty"`
	// DeliverSpam passes on mail the provider marked as spam.
	DeliverSpam bool `yaml:"deliver_spam,omitempty"`
}

// MailServer is an IMAP or SMTP server and the login there.
type MailServer struct {
	Host string `yaml:"host,omitempty"`
	Port int    `yaml:"port,omitempty"`
	// Security is "tls" (from the start, ports 993/465), "starttls"
	// (ports 143/587) or "none" (a local relay only).
	Security string `yaml:"security,omitempty"`
	User     string `yaml:"user,omitempty"`
	Password string `yaml:"password,omitempty"`
	// Folder is the IMAP folder to fetch; "" is INBOX.
	Folder string `yaml:"folder,omitempty"`
}

// Limit is the daily limit per caller.
func (e EmailConfig) Limit() int { return intOr(e.DailyLimit, 20) }

// SecurityLevel is a named security level (0-255).
type SecurityLevel struct {
	Level int    `yaml:"level" json:"level"`
	Name  string `yaml:"name" json:"name"`
}

// Levels is c's named security levels, lowest first: the sysop's, or
// -- none set -- the ones the board itself uses, named by name (the
// waiting new user, the new user, the sysop).
func (c *Config) Levels(name func(key string) string) []SecurityLevel {
	if len(c.SecurityLevels) > 0 {
		out := append([]SecurityLevel(nil), c.SecurityLevels...)
		sort.Slice(out, func(i, j int) bool { return out[i].Level < out[j].Level })
		return out
	}
	out := []SecurityLevel{{Level: c.BBS.NewUserSL, Name: name("sl.new_user")}, {Level: 255, Name: name("sl.sysop")}}
	if c.Security.Approval() && c.Security.Pending() != c.BBS.NewUserSL {
		out = append([]SecurityLevel{{Level: c.Security.Pending(), Name: name("sl.pending")}}, out...)
	}
	return out
}
