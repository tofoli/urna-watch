package format

import "testing"

func TestVotesAndPercent(t *testing.T) {
	cases := map[int64]string{0: "0", 1000: "1.000", 193574: "193.574", 158745502: "158.745.502", -1234: "-1.234"}
	for in, want := range cases {
		if got := Votes(in); got != want {
			t.Fatalf("Votes(%d) = %q, want %q", in, got, want)
		}
	}
	percent := map[int64]string{
		0: "0,00", 245369035: "0,25", 57655409048: "57,66",
		42344590952: "42,34", 100_000_000_000: "100,00", 999_999_999: "1,00",
	}
	for in, want := range percent {
		if got := PercentNano(in); got != want {
			t.Fatalf("PercentNano(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanTextRemovesTerminalControls(t *testing.T) {
	in := "LULA\x1b[31m <b> & \u202eRTL\nnext"
	got := CleanText(in)
	if got != "LULA[31m <b> & RTL next" {
		t.Fatalf("got %q", got)
	}
	if escaped := EscapeHTML(in); escaped != "LULA[31m &lt;b&gt; &amp; RTL next" {
		t.Fatalf("escaped = %q", escaped)
	}
}
