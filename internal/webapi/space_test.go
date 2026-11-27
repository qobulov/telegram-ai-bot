package webapi

import (
	"strings"
	"testing"
)

// An object is resolved by its name and told with its facts and source; a
// name the catalogue lacks says so, with nothing made up.
func TestSpaceObject(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/space/api/v1/resolve":     ok(`{"object":{"slug":"m42"}}`),
		"/space/api/v1/objects/m42": ok(`{"slug":"m42","name":"Great Orion Nebula","category":"nebula","object_type":"Nebula with cluster","mag":4,"distance_ly":1344,"constellation":"Orion","discovered":"1610","description":"The nearest stellar nursery.","source":{"name":"OpenNGC","license":"CC-BY-SA-4.0"}}`),
	})
	env := run(t, u.options(), "space", `{"action":"object","name":"M42"}`)
	for _, want := range []string{`"name":"Great Orion Nebula"`, `"distance_ly":1344`, `"constellation":"Orion"`, "OpenNGC (CC-BY-SA-4.0)", "The nearest stellar nursery."} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("object lacks %s: %s", want, env.raw)
		}
	}
	missing := newUpstream(t, map[string]stub{
		"/space/api/v1/resolve": ok(`{"object":null}`),
		"/space/api/v1/search":  ok(`{"results":[]}`),
	})
	if env = run(t, missing.options(), "space", `{"action":"object","name":"Planet Nine"}`); !strings.Contains(env.raw, `"found":false`) {
		t.Errorf("a missing object = %s", env.raw)
	}
}

// Events come with Tashkent's times; what is up now leaves the Sun out and
// says where each object is.
func TestSpaceSky(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/space/api/v1/events":  ok(`{"events":[{"kind":"conjunction","time":"2026-11-30T08:25:00.461Z","title":"The Moon 1.1° from Jupiter","summary":"Before dawn."}]}`),
		"/space/api/v1/visible": ok(`{"sun":{"condition":"night"},"objects":[{"name":"Sun","object_type":"Star"},{"name":"Jupiter","object_type":"Gas giant","mag":-2.5,"altitude_deg":40.4,"azimuth_deg":95}]}`),
		"/space/api/v1/resolve": ok(`{"object":{"slug":"jupiter"}}`),
		"/space/api/v1/ephemeris": ok(`{"target":{"name":"Jupiter"},"night":{"sunset":"2026-11-27T11:55:55Z"},"events":{"rise":"2026-11-27T18:01:23Z","set":null,"transit":"2026-11-28T00:51:44Z","transit_altitude_deg":62.06},
			"observability":{"hours_observable":4.92,"best":{"time":"2026-11-28T00:47:52Z"}},"moon":{"phase_name":"waning gibbous","illumination":0.8564}}`),
	})
	env := run(t, u.options(), "space", `{"action":"events","days":10}`)
	if !env.OK || !strings.Contains(env.raw, `"when_tashkent":"30 Nov 13:25"`) || !strings.Contains(env.raw, "Jupiter") {
		t.Errorf("events = %s", env.raw)
	}
	env = run(t, u.options(), "space", `{"action":"visible"}`)
	if !env.OK || strings.Contains(env.raw, `"Sun"`) || !strings.Contains(env.raw, `"direction":"sharq"`) || !strings.Contains(env.raw, `"altitude_deg":40`) {
		t.Errorf("visible = %s", env.raw)
	}
	env = run(t, u.options(), "space", `{"action":"tonight","name":"Jupiter"}`)
	for _, want := range []string{`"rises":"27 Nov 23:01"`, `"highest":"28 Nov 05:51"`, `"sets":""`, `"highest_altitude_deg":62`, "waning gibbous, 86% lit"} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("tonight lacks %s: %s", want, env.raw)
		}
	}
	if env = run(t, u.options(), "space", `{"action":"comet"}`); env.OK {
		t.Errorf("an unknown action = %s", env.raw)
	}
}
