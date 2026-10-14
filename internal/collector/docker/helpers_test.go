package docker

import "testing"

func TestParseWatchFilter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		wantAll bool
		wantKey string
		wantVal string
		wantPre string
		wantErr bool
	}{
		{"all", true, "", "", "", false},
		{"label:opspilot.watch=true", false, "opspilot.watch", "true", "", false},
		{"name:demo-", false, "", "", "demo-", false},
		{"bad:filter", false, "", "", "", true},
	}
	for _, tc := range cases {
		f, err := parseWatchFilter(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if f.all != tc.wantAll || f.labelKey != tc.wantKey || f.labelVal != tc.wantVal || f.namePrefix != tc.wantPre {
			t.Fatalf("%q => %+v", tc.in, f)
		}
	}
}

func TestWatchFilterMatches(t *testing.T) {
	t.Parallel()
	labelFilter, _ := parseWatchFilter("label:opspilot.watch=true")
	nameFilter, _ := parseWatchFilter("name:demo-")

	labelCtr := containerSummary{
		Names:  []string{"/ticket-api"},
		Labels: map[string]string{"opspilot.watch": "true"},
	}
	if !labelFilter.matches(labelCtr) {
		t.Fatal("expected label match")
	}
	if nameFilter.matches(labelCtr) {
		t.Fatal("expected no name match")
	}

	nameCtr := containerSummary{Names: []string{"/demo-api"}}
	if !nameFilter.matches(nameCtr) {
		t.Fatal("expected name prefix match")
	}
}

func TestContainerName(t *testing.T) {
	t.Parallel()
	if got := containerName(containerSummary{Names: []string{"/foo"}}); got != "foo" {
		t.Fatalf("got %q", got)
	}
}

func TestDemuxDockerLogs(t *testing.T) {
	t.Parallel()
	plain := []byte("hello world")
	if got := string(demuxDockerLogs(plain)); got != "hello world" {
		t.Fatalf("plain: got %q", got)
	}
	muxed := append([]byte{2, 0, 0, 0, 0, 0, 0, 5}, []byte("error")...)
	muxed = append(muxed, append([]byte{1, 0, 0, 0, 0, 0, 0, 5}, []byte("hello")...)...)
	if got := string(demuxDockerLogs(muxed)); got != "errorhello" {
		t.Fatalf("muxed: got %q", got)
	}
}

func TestSanitizeUTF8(t *testing.T) {
	t.Parallel()
	if got := sanitizeUTF8("ok"); got != "ok" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeUTF8("a\xFFb"); got != "ab" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeUTF8("a\x00b"); got != "ab" {
		t.Fatalf("null: got %q", got)
	}
}

func TestSocketPathFromHost(t *testing.T) {
	t.Parallel()
	p, err := socketPathFromHost("unix:///var/run/docker.sock")
	if err != nil || p != "/var/run/docker.sock" {
		t.Fatalf("got %q err=%v", p, err)
	}
	if _, err := socketPathFromHost("tcp://127.0.0.1:2375"); err == nil {
		t.Fatal("expected error for tcp host")
	}
}
