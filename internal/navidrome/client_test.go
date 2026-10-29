package navidrome_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/rairulyle/bitchord-selfhosted-addon/internal/fakes"
	"github.com/rairulyle/bitchord-selfhosted-addon/internal/media"
	"github.com/rairulyle/bitchord-selfhosted-addon/internal/navidrome"
)

func signedIn(fake *fakes.Navidrome, pageSize int) *navidrome.Client {
	credential := navidrome.Credential(fakes.NavidromeUser, fakes.NavidromePassword)
	return navidrome.New(navidrome.Options{BaseURL: fake.URL, Credential: credential, PageSize: pageSize})
}

func keys(tracks []media.Track) string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.ID
	}
	return strings.Join(out, ",")
}

const music = fakes.NavidromeFlacID + "," + fakes.NavidromeALACID + "," + fakes.NavidromeMP3ID

func TestAllTracksPagesThroughEverySong(t *testing.T) {
	fake := fakes.NewNavidrome(t)
	tracks, err := signedIn(fake, 2).AllTracks(context.Background(), "")
	if err != nil {
		t.Fatalf("AllTracks: %v", err)
	}
	if got := keys(tracks); got != music+","+fakes.NavidromeBookID {
		t.Fatalf("keys = %s", got)
	}
	pages := 0
	for _, r := range fake.Requests() {
		if r.Path == "/rest/search3" {
			pages++
			if strings.Contains(r.Query, "musicFolderId") {
				t.Errorf("an unfiltered listing named a folder: %s", r.Query)
			}
		}
	}
	if pages != 3 {
		t.Errorf("listed in %d pages, want 3 (2+2+1)", pages)
	}
}

func TestAllTracksFiltersLibraries(t *testing.T) {
	cases := map[string]struct{ filter, want string }{
		"by id":   {"2", fakes.NavidromeBookID},
		"by name": {"Music", music},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tracks, err := signedIn(fakes.NewNavidrome(t), 500).AllTracks(context.Background(), tc.filter)
			if err != nil {
				t.Fatalf("AllTracks: %v", err)
			}
			if got := keys(tracks); got != tc.want {
				t.Fatalf("keys = %s, want %s", got, tc.want)
			}
		})
	}
	_, err := signedIn(fakes.NewNavidrome(t), 500).AllTracks(context.Background(), "Nope")
	if err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Errorf("err = %v, want it to name the filter", err)
	}
}

func TestAllTracksTerminatesWhenPagingIsIgnored(t *testing.T) {
	fake := fakes.NewNavidrome(t)
	fake.IgnorePaging = true
	tracks, err := signedIn(fake, 2).AllTracks(context.Background(), "Music")
	if err != nil {
		t.Fatalf("AllTracks: %v", err)
	}
	if got := keys(tracks); got != music {
		t.Fatalf("keys = %s", got)
	}
}

func TestSongsConvert(t *testing.T) {
	tracks, err := signedIn(fakes.NewNavidrome(t), 500).AllTracks(context.Background(), "Music")
	if err != nil {
		t.Fatal(err)
	}
	want := []media.Track{
		{ID: fakes.NavidromeFlacID, Title: "New Religion", Artist: "All Time Low, Teddy Swims", AlbumArtist: "All Time Low", Album: "Tell Me I’m Alive", DurationSec: 184,
			Codec: "flac", Container: "flac", BitrateKbps: 1875, SampleRate: 48000, BitDepth: 24,
			FileRef: "/rest/stream?format=raw&id=" + fakes.NavidromeFlacID, ArtRef: "/rest/getCoverArt?id=al-" + fakes.NavidromeFlacID + "&size=600"},
		{ID: fakes.NavidromeALACID, Title: "Closer", Artist: "Anberlin", Album: "As You Found Me", DurationSec: 230,
			Codec: "alac", Container: "mp4", BitrateKbps: 900, SampleRate: 44100, BitDepth: 16,
			FileRef: "/rest/stream?format=raw&id=" + fakes.NavidromeALACID},
		{ID: fakes.NavidromeMP3ID, Title: "Endless Slaughter", Artist: "Limp Bizkit", Album: "Endless Slaughter", DurationSec: 337,
			Codec: "mp3", Container: "mp3", BitrateKbps: 320, SampleRate: 44100,
			FileRef: "/rest/stream?format=raw&id=" + fakes.NavidromeMP3ID, ArtRef: "/rest/getCoverArt?id=mf-" + fakes.NavidromeMP3ID + "&size=600"},
	}
	if !reflect.DeepEqual(tracks, want) {
		t.Fatalf("tracks =\n%+v\nwant\n%+v", tracks, want)
	}
}

func TestSignInSendsATokenAndNeverThePassword(t *testing.T) {
	fake := fakes.NewNavidrome(t)
	session, err := navidrome.SignIn(context.Background(), fake.URL, fakes.NavidromeUser, fakes.NavidromePassword)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if session.Username != fakes.NavidromeUser || strings.Contains(session.Credential, fakes.NavidromePassword) || strings.Contains(session.Credential, "correct") {
		t.Fatalf("session = %+v", session)
	}
	c := navidrome.New(navidrome.Options{BaseURL: fake.URL, Credential: session.Credential})
	if _, err := c.AllTracks(context.Background(), ""); err != nil {
		t.Fatalf("the stored credential does not work: %v", err)
	}
	for _, r := range fake.Requests() {
		if strings.Contains(r.Query, "p=") || strings.Contains(r.Query, "correct") {
			t.Errorf("%s?%s: the password was sent", r.Path, r.Query)
		}
	}
	_, err = navidrome.SignIn(context.Background(), fake.URL, fakes.NavidromeUser, "nope")
	if !errors.Is(err, media.ErrUnauthorized) {
		t.Fatalf("wrong password: err = %v", err)
	}
}

func TestEachSignInGetsItsOwnSalt(t *testing.T) {
	a, b := navidrome.Credential("u", "pw"), navidrome.Credential("u", "pw")
	if a == b {
		t.Fatal("two credentials share a salt")
	}
	if _, _, _, ok := navidrome.ParseCredential(a); !ok {
		t.Fatalf("ParseCredential(%q) failed", a)
	}
	if _, _, _, ok := navidrome.ParseCredential(fakes.NavidromeAPIKey); ok {
		t.Fatal("an API key parsed as a sign-in")
	}
}

func TestAPIKeyIsSentAlone(t *testing.T) {
	fake := fakes.NewNavidrome(t)
	c := navidrome.New(navidrome.Options{BaseURL: fake.URL, Credential: fakes.NavidromeAPIKey})
	version, err := c.Version(context.Background())
	if err != nil || version != fakes.NavidromeVersion {
		t.Fatalf("Version = %q, %v", version, err)
	}
	if _, err := c.Libraries(context.Background()); err != nil {
		t.Fatal(err)
	}
	bad := navidrome.New(navidrome.Options{BaseURL: fake.URL, Credential: "nds_wrong"})
	if _, err := bad.Libraries(context.Background()); !errors.Is(err, media.ErrUnauthorized) {
		t.Fatalf("a wrong key: err = %v", err)
	}
}

func TestTrack(t *testing.T) {
	c := signedIn(fakes.NewNavidrome(t), 500)
	track, err := c.Track(context.Background(), fakes.NavidromeMP3ID)
	if err != nil || track.Title != "Endless Slaughter" || !track.Playable() {
		t.Fatalf("Track = %+v, %v", track, err)
	}
	for _, id := range []string{"missing", "dir-1"} {
		if _, err := c.Track(context.Background(), id); !errors.Is(err, media.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestOpenFileAsksForTheOriginalAndPassesRanges(t *testing.T) {
	fake := fakes.NewNavidrome(t)
	c := signedIn(fake, 500)
	track, err := c.Track(context.Background(), fakes.NavidromeFlacID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.OpenFile(context.Background(), track, http.MethodGet, http.Header{"Range": {"bytes=10-19"}})
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusPartialContent || string(body) != "abcdefghij" || res.Header.Get("Content-Type") != "audio/flac" {
		t.Fatalf("status %d, body %q, type %q", res.StatusCode, body, res.Header.Get("Content-Type"))
	}
}

func TestOpenFileTurnsAnErrorAnswerIntoAnError(t *testing.T) {
	fake := fakes.NewNavidrome(t)
	c := signedIn(fake, 500)
	gone := media.Track{ID: "missing", FileRef: navidrome.FilePath("missing")}
	if _, err := c.OpenFile(context.Background(), gone, http.MethodGet, nil); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("missing file: err = %v, want ErrNotFound", err)
	}
	expired := navidrome.New(navidrome.Options{BaseURL: fake.URL, Credential: navidrome.Credential(fakes.NavidromeUser, "changed")})
	track := media.Track{ID: fakes.NavidromeFlacID, FileRef: navidrome.FilePath(fakes.NavidromeFlacID)}
	if _, err := expired.OpenFile(context.Background(), track, http.MethodGet, nil); !errors.Is(err, media.ErrUnauthorized) {
		t.Fatalf("changed password: err = %v, want ErrUnauthorized", err)
	}
}

func TestOpenArt(t *testing.T) {
	c := signedIn(fakes.NewNavidrome(t), 500)
	track, err := c.Track(context.Background(), fakes.NavidromeFlacID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.OpenArt(context.Background(), track)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if body, _ := io.ReadAll(res.Body); string(body) != "jpeg:al-"+fakes.NavidromeFlacID+"@600" {
		t.Fatalf("art = %q", body)
	}
}

func TestTransportErrorsDoNotCarryTheCredential(t *testing.T) {
	credential := navidrome.Credential(fakes.NavidromeUser, fakes.NavidromePassword)
	_, token, _, _ := navidrome.ParseCredential(credential)
	for _, c := range []*navidrome.Client{
		navidrome.New(navidrome.Options{BaseURL: fakes.DeadURL(t), Credential: credential}),
		navidrome.New(navidrome.Options{BaseURL: fakes.DeadURL(t), Credential: fakes.NavidromeAPIKey}),
	} {
		_, err := c.Libraries(context.Background())
		if err == nil {
			t.Fatal("a dead server answered")
		}
		if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), fakes.NavidromeAPIKey) {
			t.Fatalf("error carries the credential: %v", err)
		}
	}
}
