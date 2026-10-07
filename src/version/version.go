package version

var Branch = "unknown"
var Commit = "unknown"
var Time = "unknown"

type BuildInfo struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Time   string `json:"time"`
}

func GetBuildInfo() BuildInfo {
	return BuildInfo{Branch: Branch, Commit: Commit, Time: Time}
}
