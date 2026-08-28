package stash

import "testing"

func TestParseFilename(t *testing.T) {
	cases := []struct {
		name      string
		basename  string
		wantDate  string
		wantTitle string
		wantPerf  []string
	}{
		{
			name:      "standard single performer",
			basename:  "2024-12-15_Some.Performer-A.Long.Scene.Title.With.Words_1080p.mp4",
			wantDate:  "2024-12-15",
			wantTitle: "A Long Scene Title With Words",
			wantPerf:  []string{"Some Performer"},
		},
		{
			name:      "title with dash",
			basename:  "2023-10-24_Codi.Vore-How.Women.Orgasm.-.Codi.Vore_1080p.mp4",
			wantDate:  "2023-10-24",
			wantTitle: "How Women Orgasm - Codi Vore",
			wantPerf:  []string{"Codi Vore"},
		},
		{
			name:      "4k resolution",
			basename:  "2022-08-24_Angel.The.Dreamgirl-716.She.Makes.You.Hungry_4k.mp4",
			wantDate:  "2022-08-24",
			wantTitle: "716 She Makes You Hungry",
			wantPerf:  []string{"Angel The Dreamgirl"},
		},
		{
			name:      "8k resolution",
			basename:  "2025-07-03_Gigi.Dior-Breeding.Gigi.Dior_8k.mp4",
			wantDate:  "2025-07-03",
			wantTitle: "Breeding Gigi Dior",
			wantPerf:  []string{"Gigi Dior"},
		},
		{
			name:      "import suffix",
			basename:  "2024-12-15_Some.Performer-A.Long.Scene.Title_1080p_1.mp4",
			wantDate:  "2024-12-15",
			wantTitle: "A Long Scene Title",
			wantPerf:  []string{"Some Performer"},
		},
		{
			name:      "performers omitted (dash at start)",
			basename:  "2023-10-24_-Group.Title_1080p.mp4",
			wantDate:  "2023-10-24",
			wantTitle: "Group Title",
			wantPerf:  nil,
		},
		{
			name:      "dot separators in date",
			basename:  "2024.12.15_Performer-Title_1080p.mp4",
			wantDate:  "2024-12-15",
			wantTitle: "Title",
			wantPerf:  []string{"Performer"},
		},
		{
			name:      "mkv extension",
			basename:  "2024-12-15_Foo-Bar_720p.mkv",
			wantDate:  "2024-12-15",
			wantTitle: "Bar",
			wantPerf:  []string{"Foo"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pf, ok := ParseFilename(c.basename)
			if !ok {
				t.Fatalf("ParseFilename(%q) returned ok=false", c.basename)
			}
			if pf.Date != c.wantDate {
				t.Errorf("Date = %q, want %q", pf.Date, c.wantDate)
			}
			if pf.Title != c.wantTitle {
				t.Errorf("Title = %q, want %q", pf.Title, c.wantTitle)
			}
			if len(pf.Performers) != len(c.wantPerf) {
				t.Fatalf("Performers = %v, want %v", pf.Performers, c.wantPerf)
			}
			for i, want := range c.wantPerf {
				if pf.Performers[i] != want {
					t.Errorf("Performers[%d] = %q, want %q", i, pf.Performers[i], want)
				}
			}
		})
	}
}

func TestParseFilenameReturnsFalseForJunk(t *testing.T) {
	junk := []string{
		"random_garbage_4k_encoded.mp4",
		"🎄No PPV 🎄 Justine Jakobs 🌽 OnlyFans.mp4",
		"tbkjnjkvfucrkfobvnuewewtgwiihcdg.mp4",
		"Gigi Dior - Oil Anal and MILF - Big Wet Tits 24 - ELEGANT ANGEL.mp4",
	}
	for _, name := range junk {
		if pf, ok := ParseFilename(name); ok {
			t.Errorf("expected ok=false for junk %q, got %+v", name, pf)
		}
	}
}

func TestParseFilenameMultiplePerformers(t *testing.T) {
	pf, ok := ParseFilename("2024-01-01_Alice_Bob.Smith-Some.Title_1080p.mp4")
	if !ok {
		t.Fatal("ok = false")
	}
	if len(pf.Performers) != 2 {
		t.Fatalf("expected 2 performers, got %v", pf.Performers)
	}
	if pf.Performers[0] != "Alice" {
		t.Errorf("Performers[0] = %q, want Alice", pf.Performers[0])
	}
	if pf.Performers[1] != "Bob Smith" {
		t.Errorf("Performers[1] = %q, want Bob Smith", pf.Performers[1])
	}
}
