package lastcallers

import (
	"testing"
	"time"
)

// Real records from fsxNet's FSX_DAT (2026-09-30).
const xbit = ">>> BEGIN\n~D:C@?\n%96 )\\q:E qq$\n_h^b_^ae\n_fi_c2\nqF6?2 !2C<[ rp\n(:?5@HD\nI\\3:E]@C8\n>>> END\n--- SBBSecho 3.37-Win32\n * Origin: X-Bit BBS (21:4/107)\n"

const futureland = "BEGIN\n6A@49M2=:2DM4:EJM4@F?ECJM4=:6?EM5@@CM33DMFC=MDJDE6>\n`fh_fefe_fM2D5MM%FCE=6 xD=2?5M%6=?6E k?@ ?2>6m A@CE dgfa_ 2?D:\\33DM4FDE@>=@8@?];DM7FEFC6=2?5]E@52JM7FEFC6=2?5]E@52JiabM{:?FI\nEND\n--- SBBSecho 3.37-Linux\n"

func TestParseCommonFormat(t *testing.T) {
	got := Parse(xbit)
	want := Record{Alias: "Osiron", BBS: "The X-Bit BBS", Date: "09/30/26", Time: "07:04a", Location: "Buena Park, CA", System: "Windows", Address: "x-bit.org"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Parse = %+v, want %+v", got, want)
	}
}

func TestParseFuturelandFormat(t *testing.T) {
	got := Parse(futureland)
	if len(got) != 1 || got[0].Alias != "asd" || got[0].BBS != "futureland.today" || got[0].Location != "Turtle Island" ||
		got[0].System != "Linux" || got[0].Address != "futureland.today:23" || got[0].Date == "" {
		t.Fatalf("Parse = %+v", got)
	}
}

func TestEncodeRoundTripsAndMatchesTheCommonLayout(t *testing.T) {
	when := time.Date(2026, 9, 30, 13, 5, 0, 0, time.UTC)
	r := Record{Alias: "SwissMaik", BBS: "Maiks Place BBS", Date: FormatDate(when), Time: FormatTime(when), Location: "Neunkirch, CH", System: "Linux", Address: "bbs.maik.ch:2323"}
	if r.Time != "01:05p" || r.Date != "09/30/26" {
		t.Fatalf("date/time = %q %q, want 09/30/26 01:05p", r.Date, r.Time)
	}
	if got := Parse(Encode(r)); len(got) != 1 || got[0] != r {
		t.Fatalf("round trip = %+v, want %+v", got, r)
	}
	if FormatTime(time.Date(2026, 1, 1, 0, 7, 0, 0, time.UTC)) != "12:07a" || FormatTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) != "12:00p" {
		t.Fatal("midnight/noon wrong")
	}
}
