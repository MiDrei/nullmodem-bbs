package doors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Template is a ready-made door setup for a known door: how to launch
// it (natively, or under DOSBox-X), which drop file it wants and where,
// and -- for doors whose license allows redistribution -- where to
// download it from. The web admin offers these so a sysop doesn't have
// to work out each door's command line and drop file quirks by hand.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// License is shown next to the door. For a door without Download,
	// the sysop has to get it themselves.
	License string `json:"license"`
	// Dir is the directory under the doors dir the door lives in.
	Dir string `json:"dir"`
	// Kind is "dosbox" (the default) or "native" -- see Door.Kind.
	Kind string `json:"kind"`
	// Exe (relative to Dir), Args and Stdio are a native door's; see
	// Door.
	Exe               string   `json:"exe,omitempty"`
	Args              []string `json:"args,omitempty"`
	Stdio             bool     `json:"stdio,omitempty"`
	ANSI16            bool     `json:"ansi16,omitempty"`
	DOSBoxLaunchCmd   string   `json:"dosbox_launch_cmd,omitempty"`
	DropFile          string   `json:"dropfile"`
	DropFileInDoorDir bool     `json:"dropfile_in_door_dir"`
	LockFiles         []string `json:"lock_files,omitempty"`
	// Setup is what the sysop still has to do once after installing,
	// if anything (typically running the door's own setup program from
	// the "DOS Shell (Doorway)" door).
	Setup string `json:"setup,omitempty"`
	// Download is nil for doors that must be installed by hand.
	Download *Download `json:"download,omitempty"`
	// SourceURL is where the door comes from, for the admin to link.
	SourceURL string `json:"source_url,omitempty"`
	// Program is the door's background program, if it has one -- see
	// Program and Supervisor.
	Program []string `json:"program,omitempty"`
	// MRC marks uMRC: installing it asks for what the chat network
	// shows about this board (see MRCConfig).
	MRC bool `json:"mrc,omitempty"`
	// Bulletins are the files the door writes for the board (scores,
	// news), relative to its directory; EnableBulletins, if set, turns
	// their writing on in an installed door (dir is its directory).
	Bulletins       []Bulletin                      `json:"bulletins,omitempty"`
	EnableBulletins func(dir, bbsName string) error `json:"-"`
	// Daily is the daily maintenance command it should get, if any.
	Daily string `json:"daily,omitempty"`
}

// Bulletin is a file a door writes for the board.
type Bulletin struct {
	Title  string `json:"title"`
	File   string `json:"file"`
	Public bool   `json:"public"`
}

// TemplateFor is the template door was installed from: by its
// template ID, else by its directory's name.
func TemplateFor(id, dir string) (Template, bool) {
	base := filepath.Base(filepath.Clean(dir))
	for _, t := range Templates {
		if (id != "" && t.ID == id) || (id == "" && dir != "" && t.Dir == base) {
			return t, true
		}
	}
	return Template{}, false
}

// Download says how Install fetches and unpacks a door.
type Download struct {
	// URL may contain "{arch}", filled from Arch, and "{version}", the
	// release's tag.
	URL string `json:"url"`
	// Version is the release Install fetches unless told otherwise --
	// the newest known when the template was written.
	Version string `json:"version,omitempty"`
	// GitHub is the door's "owner/repo" on GitHub, for the update
	// check: its latest release's tag is the newest version.
	GitHub string `json:"-"`
	// LegacyVersion is what an install without a version file (one
	// from before they were written) has: the version the template
	// installed then.
	LegacyVersion string `json:"-"`
	// Arch maps Go's GOARCH to the name the door's releases use for it
	// ("amd64" -> "x86_64"), for URL and Subdir. Empty for a download
	// that's the same everywhere (DOS doors); a GOARCH missing from a
	// non-empty map has no build.
	Arch map[string]string `json:"-"`
	// Format is "zip" or "tar.gz".
	Format string `json:"format"`
	// Subdir, if set, is the directory inside the archive the door's
	// files are in (e.g. "JudgeDredd-main/GAME"); only it is unpacked,
	// flattened into the door's own directory. May contain "{arch}".
	Subdir string `json:"subdir,omitempty"`
	// Executables are made executable after unpacking, for archives
	// that don't record it (relative to the door's directory).
	Executables []string `json:"-"`
	// CtlFile is a DDPlus-style control file whose SYSOPFIRST,
	// SYSOPLAST and BBSNAME lines are set to this board's own.
	CtlFile string `json:"-"`
	// Prepare, if set, runs last on the unpacked door directory, for
	// door-specific setup the fields above don't cover.
	Prepare func(ctx context.Context, dir, bbsName, sysopName string) error `json:"-"`
}

// Templates are the doors the web admin offers ready-made. The DOS
// command lines follow each door's own documentation; FOSSIL comes
// from DOSBox-X itself (see dosboxConfigTemplate), so none of them
// load BNU or X00.
var Templates = []Template{
	{
		ID:          "umrc",
		Name:        "MRC Chat",
		Description: "Multi-Relay Chat: live chat with the callers of 180+ other BBSes, through uMRC.",
		License:     "MIT",
		Dir:         "umrc",
		Kind:        "native",
		Exe:         "umrc-client",
		// -IP lets the chat network ban a single troublemaker instead
		// of the whole board.
		Args:    []string{"-D", "{dropfile}", "-IP{ip}"},
		Program: []string{"umrc-bridge"},
		MRC:     true,
		Download: &Download{
			URL:           "https://github.com/codefenix-dev/uMRC/releases/download/{version}/umrc-{version}-linux-{arch}.tar.gz",
			Version:       "106",
			GitHub:        "codefenix-dev/uMRC",
			LegacyVersion: "106",
			Arch:          map[string]string{"amd64": "x64", "arm64": "arm64"},
			Format:        "tar.gz",
			Executables:   []string{"umrc-client", "umrc-bridge", "setup"},
		},
		Setup:     `Its background program umrc-bridge keeps the connection to the chat network and runs as long as the door is set up (see Services). Only one such connection per board is allowed: don't run uMRC for the same board anywhere else.`,
		SourceURL: "https://github.com/codefenix-dev/uMRC",
	},
	{
		ID:          "immortal-barons",
		Name:        "Immortal Barons",
		Description: "Build an empire and conquer your neighbours: a faithful remake of Barren Realms Elite, with inter-BBS leagues.",
		License:     "MIT",
		Dir:         "immortal-barons",
		Kind:        "native",
		Exe:         "immortal-barons",
		Args:        []string{"-dropfile", "{dropfile}", "-data", "data"},
		// Its title art is drawn in 256 colours, which classic BBS
		// terminals turn into stripes.
		ANSI16: true,
		Download: &Download{
			URL:           "https://github.com/andy5995/immortal-barons/releases/download/{version}/immortal-barons-{version}-linux-{arch}.tar.gz",
			Version:       "v0.2.3",
			GitHub:        "andy5995/immortal-barons",
			LegacyVersion: "v0.2.0",
			Arch:          map[string]string{"amd64": "amd64", "arm64": "arm64"},
			Format:        "tar.gz",
			Subdir:        "immortal-barons-{version}-linux-{arch}",
			Prepare:       prepareImmortalBarons,
		},
		Setup:     `Installed with the default game settings. To change them later (turns per day and so on), run "immortal-barons -reset -data data" in the door's directory -- that also starts a new game.`,
		SourceURL: "https://github.com/andy5995/immortal-barons",
		// Written on each game day (the daily maintenance, or the
		// first login of a day), into BulletinDir.
		Bulletins: []Bulletin{
			{Title: "Immortal Barons: scoreboard", File: "data/bull/scores.ans", Public: true},
			{Title: "Immortal Barons: today's news", File: "data/bull/tdynews.ans"},
			{Title: "Immortal Barons: yesterday's news", File: "data/bull/yesnews.ans"},
		},
		EnableBulletins: enableImmortalBaronsBulletins,
		Daily:           "immortal-barons -maint -data data",
	},
	{
		ID:          "usurper-reborn",
		Name:        "Usurper Reborn",
		Description: "The big modern Usurper: a persistent fantasy RPG with 60+ NPCs who live, marry and die on their own.",
		License:     "GPL-2.0",
		Dir:         "usurper-reborn",
		Kind:        "native",
		Exe:         "UsurperReborn",
		Args:        []string{"--door32", "{dropfile}"},
		// It switches to standard I/O by itself once its output is
		// redirected, so it's run that way.
		Stdio: true,
		Download: &Download{
			URL:           "https://github.com/binary-knight/usurper-reborn/releases/download/{version}/UsurperReborn-{version}-Linux-{arch}.zip",
			Version:       "v1.2.7",
			GitHub:        "binary-knight/usurper-reborn",
			LegacyVersion: "v1.1.14",
			Arch:          map[string]string{"amd64": "x64", "arm64": "ARM64"},
			Format:        "zip",
			Executables:   []string{"UsurperReborn"},
		},
		Setup:     `A large download (about 60 MB). Players can also reach the game's public online server from its menu.`,
		SourceURL: "https://github.com/binary-knight/usurper-reborn",
		Bulletins: []Bulletin{{Title: "Usurper Reborn: news", File: "SCORES/NEWS.txt", Public: true}},
	},
	{
		ID:          "usurper",
		Name:        "Usurper",
		Description: "The 1993 original: dungeons, monsters, player combat, gods and marriages. Native Linux build of version 0.25.",
		License:     "GPL-2.0",
		Dir:         "usurper",
		Kind:        "native",
		Exe:         "USURPER.EXE",
		// No placeholder: the door gets Usurper's own "/P<dir>/" switch.
		// Its SYSOP.TXT: ONLINERS.DAT left over from a crashed session
		// shows that caller as still playing, and is "perfectly safe to
		// erase ... when nobody really is playing".
		LockFiles: []string{"NODE/ONLINERS.DAT"},
		Download: &Download{
			URL:         "https://github.com/rickparrish/Usurper/releases/download/latest/usurper-{arch}-linux.zip",
			Arch:        map[string]string{"amd64": "x86_64"},
			Format:      "zip",
			Executables: []string{"USURPER.EXE", "EDITOR.EXE"},
			Prepare:     prepareUsurper,
		},
		SourceURL: "https://github.com/rickparrish/Usurper",
	},
	{
		ID:          "judge-dredd",
		Name:        "Judge Dredd",
		Description: "LORD-style RPG in Mega-City One: fight perps, level up your Judge, patrol the sectors.",
		License:     "MIT",
		Dir:         "dredd",
		// DDPlus doors take the drop file's directory with /P.
		DOSBoxLaunchCmd: `DREDD /P{dropfile_dir}`,
		DropFile:        DropFileDoorSys,
		Download: &Download{
			URL:     "https://github.com/GrumpyGrendil/JudgeDredd/archive/refs/heads/main.tar.gz",
			Format:  "tar.gz",
			Subdir:  "JudgeDredd-main/GAME",
			CtlFile: "JUDGE.CTL",
			Prepare: prepareJudgeDredd,
		},
		SourceURL: "https://github.com/GrumpyGrendil/JudgeDredd",
	},
	{
		ID:                "lord",
		Name:              "Legend of the Red Dragon",
		Description:       "The best-known door game of all: slay the Red Dragon, flirt at the inn, fight other players.",
		License:           "Get it yourself (v4.07)",
		Dir:               "lord",
		DOSBoxLaunchCmd:   `CALL START.BAT {node}`,
		DropFile:          DropFileDorInfo,
		DropFileInDoorDir: true,
		Setup:             `Unpack LORD into the door's directory, then once run LORDCFG from the "DOS Shell (Doorway)" door.`,
	},
	{
		ID:                "tw2002",
		Name:              "TradeWars 2002",
		Description:       "Space trading and conquest across a galaxy of sectors, the multiplayer classic.",
		License:           "Get it yourself (v3.x)",
		Dir:               "tw2002",
		DOSBoxLaunchCmd:   `TW2002.EXE TWNODE={node} SHARE`,
		DropFile:          DropFileDorInfo,
		DropFileInDoorDir: true,
		Setup:             `Unpack TradeWars into the door's directory, then once run its setup (TEDIT/BIGBANG) from the "DOS Shell (Doorway)" door.`,
	},
	{
		ID:          "oo2",
		Name:        "Operation: Overkill II",
		Description: "Post-apocalyptic survival RPG.",
		License:     "Get it yourself (v1.21)",
		Dir:         "oo2",
		// OOINFO converts the drop file into OO2's own BBSINFO.OO.
		DOSBoxLaunchCmd: "OOINFO.EXE 2 {dropfile_dir} {node}\nOOII.EXE",
		DropFile:        DropFileDoorSys,
		LockFiles:       []string{"OONODE.DAT", "BBSINFO.OO"},
		Setup:           `Unpack OO2 into the door's directory, then once run its setup from the "DOS Shell (Doorway)" door.`,
	},
	{
		ID:              "doormud",
		Name:            "DoorMUD",
		Description:     "A multi-user dungeon as a door: explore, fight and chat with everyone online.",
		License:         "Get it yourself (v0.99)",
		Dir:             "doormud",
		DOSBoxLaunchCmd: `DMUD.EXE -n {node} -d {dropfile_dir}`,
		DropFile:        DropFileDorInfo,
		Setup:           `Unpack DoorMUD into the door's directory; "DMUD.EXE -l" from the "DOS Shell (Doorway)" door configures it.`,
	},
}

// TemplateByID returns the template with id, if there is one.
func TemplateByID(id string) (Template, bool) {
	for _, t := range Templates {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// prepareJudgeDredd registers the game to this board: the title screen
// shows the BBS and sysop name from DATA/REG.DAT's first line
// ("BBS name,sysop,YYMMDD game start,999"), which ships with the
// author's own board in it.
func prepareJudgeDredd(_ context.Context, dir, bbsName, sysopName string) error {
	p, ok := findCaseInsensitive(filepath.Join(dir, "DATA"), "REG.DAT")
	if !ok {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("doors: reading REG.DAT: %w", err)
	}
	lines := strings.Split(string(data), "\r\n")
	clean := func(s string) string { return strings.ReplaceAll(s, ",", "") }
	lines[0] = fmt.Sprintf("%s,%s,%s,999", clean(bbsName), clean(sysopName), time.Now().Format("060102"))
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\r\n")), 0o644); err != nil {
		return fmt.Errorf("doors: writing REG.DAT: %w", err)
	}
	return nil
}

// prepareImmortalBarons tells the game its drop file is DOOR32.SYS
// (what -set-dropfile would ask interactively) and creates the world
// from the default settings, so the door is playable right away.
func prepareImmortalBarons(ctx context.Context, dir, _, _ string) error {
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		return fmt.Errorf("doors: %w", err)
	}
	cfg, _ := json.Marshal(map[string]string{"DropfileFormat": "door32"})
	if err := os.WriteFile(filepath.Join(data, "door.json"), cfg, 0o644); err != nil {
		return fmt.Errorf("doors: writing door.json: %w", err)
	}
	if err := enableImmortalBaronsBulletins(dir, ""); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "immortal-barons"), "-reset-from-config", "-data", "data")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("doors: creating the Immortal Barons world: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// prepareUsurper puts the sample configuration in place, set to this
// board and DOOR32.SYS, and runs "Reset Game" in Usurper's console
// EDITOR -- the only way it creates its game data.
func prepareUsurper(ctx context.Context, dir, bbsName, sysopName string) error {
	for _, name := range []string{"USURPER.CFG", "USURP.CTL"} {
		if _, ok := findCaseInsensitive(dir, name); ok {
			continue
		}
		src, ok := findCaseInsensitive(filepath.Join(dir, "SAMPLES"), name)
		if !ok {
			return fmt.Errorf("doors: Usurper's SAMPLES/%s is missing", name)
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("doors: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return fmt.Errorf("doors: %w", err)
		}
	}
	first, last := splitName(sysopName)
	if err := setCtlValues(filepath.Join(dir, "USURP.CTL"), map[string]string{
		"SYSOPFIRST": first, "SYSOPLAST": last, "BBSNAME": bbsName, "BBSTYPE": "DOOR32",
	}); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := scriptConsole(ctx, dir, filepath.Join(dir, "EDITOR.EXE"), []consoleStep{
		{Expect: "Reset Game", Send: "r"},
		{Expect: "Reset Usurper?", Send: "y"},
		{Expect: "really sure", Send: "y"},
		// Confirming the message is all; scriptConsole then ends the
		// editor, the data is written by now.
		{Expect: "has been RESET", Send: "\r"},
	})
	if err != nil {
		return fmt.Errorf("doors: resetting Usurper with EDITOR.EXE: %w", err)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "DATA")); err != nil || len(entries) == 0 {
		return errors.New("doors: Usurper's EDITOR.EXE did not create its game data")
	}
	return nil
}

// enableImmortalBaronsBulletins sets BulletinDir in data/bbs.cfg (the
// game writes its scores and news there), unless one is set already;
// the rest of the file stays as it is.
func enableImmortalBaronsBulletins(dir, bbsName string) error {
	path := filepath.Join(dir, "data", "bbs.cfg")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("doors: %w", err)
	}
	for _, line := range strings.Split(string(old), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.EqualFold(f[0], "BulletinDir") {
			return nil
		}
	}
	add := "BulletinDir      bull\n"
	if bbsName != "" && !strings.Contains(strings.ToLower(string(old)), "bbsname") {
		add += "BBSName          " + bbsName + "\n"
	}
	text := string(old)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("doors: %w", err)
	}
	if err := os.WriteFile(path, []byte(text+add), 0o644); err != nil {
		return fmt.Errorf("doors: writing bbs.cfg: %w", err)
	}
	return nil
}
