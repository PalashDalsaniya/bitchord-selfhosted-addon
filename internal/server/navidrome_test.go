package server

import (
	"log/slog"
	"net/http"
	"reflect"
	"testing"

	"github.com/rairulyle/bitchord-selfhosted-addon/internal/fakes"
	"github.com/rairulyle/bitchord-selfhosted-addon/internal/navidrome"
	"github.com/rairulyle/bitchord-selfhosted-addon/internal/store"
)

const navidromeBase = testPublic + "/navidrome-1/" + testSecret

func newNavidromeHandler(t *testing.T) http.Handler {
	t.Helper()
	fake := fakes.NewNavidrome(t)
	client := navidrome.New(navidrome.Options{BaseURL: fake.URL, Credential: navidrome.Credential(fakes.NavidromeUser, fakes.NavidromePassword)})
	log := slog.New(slog.DiscardHandler)
	return newServerWith(log, newEntry(t, "navidrome-1", store.Navidrome, "", client, log, true)).handler()
}

func TestNavidromeSearchStreamAndFile(t *testing.T) {
	h := newNavidromeHandler(t)
	got := decode[map[string]any](t, do(h, http.MethodGet, "/navidrome-1/"+testSecret+"/search?q=New+Religion+Teddy+Swims", nil))
	want := map[string]any{
		"tracks": []any{map[string]any{
			"id": fakes.NavidromeFlacID, "title": "New Religion", "artist": "All Time Low, Teddy Swims", "album": "Tell Me I’m Alive",
			"duration": float64(184), "artworkURL": navidromeBase + "/art/" + fakes.NavidromeFlacID, "format": "flac", "audioQuality": "LOSSLESS",
		}},
		"albums": []any{}, "artists": []any{}, "playlists": []any{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("search = %v", got)
	}
	descriptor := decode[map[string]any](t, do(h, http.MethodGet, "/navidrome-1/"+testSecret+"/stream/"+fakes.NavidromeFlacID, nil))
	if descriptor["url"] != navidromeBase+"/file/"+fakes.NavidromeFlacID || descriptor["quality"] != "lossless 24-bit 48kHz" || descriptor["bitrate"] != float64(1875000) {
		t.Fatalf("stream = %v", descriptor)
	}
	rec := do(h, http.MethodGet, "/navidrome-1/"+testSecret+"/file/"+fakes.NavidromeFlacID, http.Header{"Range": {"bytes=10-19"}})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "abcdefghij" || rec.Header().Get("Content-Range") != "bytes 10-19/36" {
		t.Fatalf("file: status %d, body %q, range %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Range"))
	}
	if rec := do(h, http.MethodGet, "/navidrome-1/"+testSecret+"/file/"+fakes.NavidromeALACID, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a song the server cannot stream: status %d, body %q", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/navidrome-1/"+testSecret+"/art/"+fakes.NavidromeFlacID, nil); rec.Code != http.StatusOK || rec.Body.String() != "jpeg:al-"+fakes.NavidromeFlacID+"@600" {
		t.Fatalf("art: status %d, body %q", rec.Code, rec.Body.String())
	}
}
