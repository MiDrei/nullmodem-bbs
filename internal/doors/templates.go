package doors

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Template is a ready-made door setup for a known classic door: how
// to launch it under DOSBox-X, which drop file it wants and where,
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
	Dir               string   `json:"dir"`
	DOSBoxLaunchCmd   string   `json:"dosbox_launch_cmd"`
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
}

// Download says how Install fetches and unpacks a door.
type Download struct {
	URL string `json:"url"`
	// Format is "zip" or "tar.gz".
	Format string `json:"format"`
	// Subdir, if set, is the directory inside the archive the door's
	// files are in (e.g. "JudgeDredd-main/GAME"); only it is unpacked,
	// flattened into the door's own directory.
	Subdir string `json:"subdir,omitempty"`
	// CtlFile is a DDPlus-style control file whose SYSOPFIRST,
	// SYSOPLAST and BBSNAME lines are set to this board's own.
	CtlFile string `json:"-"`
	// Prepare, if set, runs last on the unpacked door directory, for
	// door-specific setup the fields above don't cover.
	Prepare func(dir, bbsName, sysopName string) error `json:"-"`
}

// Templates are the doors the web admin offers ready-made. The DOS
// command lines follow each door's own documentation; FOSSIL comes
// from DOSBox-X itself (see dosboxConfigTemplate), so none of them
// load BNU or X00.
var Templates = []Template{
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
func prepareJudgeDredd(dir, bbsName, sysopName string) error {
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
