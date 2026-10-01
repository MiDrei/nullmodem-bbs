package nodelist

import (
	"path/filepath"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

func TestParseSetsZoneAndNet(t *testing.T) {
	list := `;A Test nodelist
Zone,21,fsxNet_ZC,Dunedin_NZL,Paul_Hayton,-Unpublished-,300,ICM,INA:net1.fsxnet.nz,IBN:24556
Host,1,fsxNet_(NET_1),Dunedin_NZL,Paul_Hayton,-Unpublished-,300,CM
Hub,100,Risa_HUB,Dunedin_NZL,Paul_Hayton,-Unpublished-,300,CM
,101,Agency_BBS,Dunedin_NZL,Paul_Hayton,-Unpublished-,300,CM,INA:ipv4.agency.bbs.nz,IBN:24555
Down,107,The_ByteXchange_BBS,Lindale_USA,Chad_Adams,-Unpublished-,300
Host,4,Net_4,Somewhere,Someone,-Unpublished-,300
,107,Other_107,Nowhere,Nobody,-Unpublished-,300
`
	es, err := Parse("fsxNet", strings.NewReader(strings.ReplaceAll(list, "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range es {
		got = append(got, e.Address()+" "+e.Name)
	}
	want := "21:21/0 fsxNet ZC|21:1/0 fsxNet (NET 1)|21:1/100 Risa HUB|21:1/101 Agency BBS|21:1/107 The ByteXchange BBS|21:4/0 Net 4|21:4/107 Other 107"
	if strings.Join(got, "|") != want {
		t.Fatalf("parsed\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
	if es[3].Host() != "ipv4.agency.bbs.nz" || es[3].Sysop != "Paul Hayton" || es[4].Keyword != "Down" {
		t.Fatalf("entry %+v", es[3])
	}
}

func TestReadRealNodelists(t *testing.T) {
	fsx, err := ReadFile("fsxNet", "FSXNET.Z75", "testdata/FSXNET.Z75")
	if err != nil || len(fsx) < 50 {
		t.Fatalf("fsxNet: %d entries, %v", len(fsx), err)
	}
	hobby, err := ReadFile("HobbyNet", "HOBBYNET.268", "testdata/HOBBYNET.268")
	if err != nil || len(hobby) < 10 {
		t.Fatalf("HobbyNet: %d entries, %v", len(hobby), err)
	}
	for _, e := range hobby {
		if strings.ContainsAny(e.Name+e.Flags, "\r\n") {
			t.Fatalf("CR left in %+v", e)
		}
	}
}

func TestStoreSyncLookupSearch(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	abs, _ := filepath.Abs("testdata/FSXNET.Z75")
	sqlDB.Exec(`INSERT INTO file_areas (id, tag, name, network) VALUES (7, 'FSX_NODE', 'fsxNet nodelist', 'fsxNet')`)
	sqlDB.Exec(`INSERT INTO files (area_id, filename, size_bytes, storage_path, uploaded_at) VALUES (7, 'FSXNET.Z68', 1, '/nonexistent', '2026-09-25 00:00:00')`)
	sqlDB.Exec(`INSERT INTO files (area_id, filename, size_bytes, storage_path, uploaded_at) VALUES (7, 'FSXNET.Z75', 1, ?, '2026-10-01 00:00:00')`, abs)
	sqlDB.Exec(`INSERT INTO files (area_id, filename, size_bytes, storage_path, uploaded_at) VALUES (7, 'readme.txt', 1, '/x', '2026-10-02 00:00:00')`)
	s := NewStore(sqlDB)
	n, err := s.Sync(false, nil)
	if err != nil || n != 1 {
		t.Fatalf("sync imported %d, %v", n, err)
	}
	if n, _ := s.Sync(false, nil); n != 0 {
		t.Fatal("the same file imported again")
	}
	e, ok, err := s.LookupAddress("21:1/101@fsxnet")
	if err != nil || !ok || e.Name != "Agency BBS" {
		t.Fatalf("lookup %+v %v %v", e, ok, err)
	}
	if _, ok, _ := s.LookupAddress("21:1/9999"); ok {
		t.Fatal("found a node that isn't listed")
	}
	if p, ok, _ := s.LookupAddress("21:1/101.5"); !ok || p.Node != 101 {
		t.Fatal("a point isn't found as its boss")
	}
	res, _ := s.Search("", "agency", 10)
	if len(res) == 0 || res[0].Address() != "21:1/101" {
		t.Fatalf("search %+v", res)
	}
	if res, _ = s.Search("", "21:1/10", 100); len(res) < 2 {
		t.Fatalf("address prefix search %+v", res)
	}
	imps, _ := s.Imports()
	if len(imps) != 1 || imps[0].Filename != "FSXNET.Z75" || imps[0].Entries < 50 {
		t.Fatalf("imports %+v", imps)
	}
}
