package olx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// AliveAtURL is the only check available for rows that predate the sweeper and
// carry no numeric id. It has to answer from the HTTP status alone, so every
// status the site can produce needs a decided meaning — a wrong reading here
// either strikes a live machine off the page or leaves a dead one on it.
func TestAliveAtURL(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/d/oferta/live-IDaaa.html":
			w.Write([]byte("<html>an ad</html>"))
		case "/d/oferta/gone-IDbbb.html":
			w.WriteHeader(http.StatusGone)
		case "/d/oferta/missing-IDccc.html":
			w.WriteHeader(http.StatusNotFound)
		case "/d/oferta/moved-IDddd.html":
			http.Redirect(w, r, srv.URL+"/", http.StatusFound)
		case "/":
			w.Write([]byte("<html>home</html>"))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := New()
	c.pause = 0

	for _, tc := range []struct {
		name      string
		path      string
		wantAlive bool
		wantErr   bool
	}{
		{"a live ad answers 200 on its own page", "/d/oferta/live-IDaaa.html", true, false},
		{"410 is the removal signal", "/d/oferta/gone-IDbbb.html", false, false},
		{"404 counts as removed too", "/d/oferta/missing-IDccc.html", false, false},
		// A 200 reached by leaving the offer path is the site saying the ad is not
		// there, in a shape that would otherwise read as "still listed".
		{"redirected off the offer page is gone", "/d/oferta/moved-IDddd.html", false, false},
		// Anything else is the site failing to answer, which must not be recorded
		// as a machine leaving the market.
		{"a server error is not an answer", "/d/oferta/boom-IDeee.html", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alive, err := c.AliveAtURL(srv.URL + tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if alive != tc.wantAlive {
				t.Errorf("alive = %v, want %v", alive, tc.wantAlive)
			}
		})
	}
}
