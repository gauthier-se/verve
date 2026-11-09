package auth

import "strings"

// UserAgent is the browser and the device that a user agent names, empty when it names none that
// ParseUserAgent knows. It is only a hint for the sessions page: a client can send any user agent.
type UserAgent struct {
	Browser string
	Device  string
}

// marker is a name that a user agent holds when it contains one of its tokens.
type marker struct {
	name   string
	tokens []string
}

// browsers are checked in order: most browsers also name the engines they are built on, such as
// Edge naming Chrome and Chrome naming Safari.
var browsers = []marker{
	{name: "Edge", tokens: []string{"Edg/", "EdgA/", "EdgiOS/"}},
	{name: "Opera", tokens: []string{"OPR/", "OPT/"}},
	{name: "Samsung Internet", tokens: []string{"SamsungBrowser/"}},
	{name: "Firefox", tokens: []string{"Firefox/", "FxiOS/"}},
	{name: "Chrome", tokens: []string{"Chrome/", "CriOS/"}},
	{name: "Safari", tokens: []string{"Safari/"}},
}

// devices are checked in order: Android names Linux, and iOS names Mac OS X.
var devices = []marker{
	{name: "iPhone", tokens: []string{"iPhone"}},
	{name: "iPad", tokens: []string{"iPad"}},
	{name: "Android", tokens: []string{"Android"}},
	{name: "ChromeOS", tokens: []string{"CrOS"}},
	{name: "Windows", tokens: []string{"Windows"}},
	{name: "Mac", tokens: []string{"Macintosh"}},
	{name: "Linux", tokens: []string{"Linux"}},
}

// ParseUserAgent returns the browser and the device of the user agent ua, such as Safari on iPhone.
func ParseUserAgent(ua string) UserAgent {
	return UserAgent{Browser: firstMarker(browsers, ua), Device: firstMarker(devices, ua)}
}

// firstMarker returns the name of the first of markers that ua holds, or empty.
func firstMarker(markers []marker, ua string) string {
	for _, m := range markers {
		for _, token := range m.tokens {
			if strings.Contains(ua, token) {
				return m.name
			}
		}
	}

	return ""
}
