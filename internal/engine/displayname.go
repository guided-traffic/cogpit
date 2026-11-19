package engine

import "strings"

// One short name for a cluster, everywhere (D-68, the owner: "überall gleich"). A kubeconfig
// context is often called "<user>@<cluster>" ("kubernetes-admin@prod-eu", the name kubeadm, kind's
// admin config and many cloud CLIs write), which leaves the part that tells clusters apart at the
// end of a long name the navigator cuts. The window names a context by ClusterName: the display
// name the user set in Cluster Settings › General, else ShortContextName of the context's name. The
// full name stays in every tooltip and in Cluster Settings; agents keep the full context ids in
// every command and in ui_state.

// ShortContextName is the name a context shows without a display name of the user's: the part of
// its kubeconfig name after the last "@" ("kubernetes-admin@prod-eu" → "prod-eu", "a@b@c" → "c",
// "@prod" → "prod"), else the whole name. A name with nothing but blanks after its last "@"
// ("admin@", "@") stays whole.
func ShortContextName(name string) string {
	i := strings.LastIndex(name, "@")
	if i < 0 {
		return name
	}
	if short := strings.TrimSpace(name[i+1:]); short != "" {
		return short
	}
	return name
}
