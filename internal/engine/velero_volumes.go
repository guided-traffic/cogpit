package engine

import (
	"cmp"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Failed volume backups on the Velero page (velero.go): a backup's errors are its failed volume
// backups, as far as Velero's objects tell. Each PodVolumeBackup (a file-system backup of a pod's
// volume, velero.io/v1) and DataUpload (the data mover's upload of a CSI snapshot,
// velero.io/v2alpha1) carries its backup's name in the label velero.io/backup-name and, failed, its
// reason in status.message: the text the backup's own counts lack, without the backup's logs (a
// DownloadRequest and the object storage, which the page never touches, D-151). The page
// collects those of the backups in Needs attention and groups them: one namespace, one way of
// failing, as their messages read once each one's own pod, volume and paths are masked.

// VeleroVolumeGroup is one row of Failed volume backups: the failed volume backups of one
// namespace whose messages are the same but for their own pod, volume and paths.
type VeleroVolumeGroup struct {
	Namespace string   `json:"namespace"`
	Volumes   int      `json:"volumes"`         // its members
	Pods      int      `json:"pods"`            // the pods they back up (a data upload names a PVC, no pod)
	Nodes     []string `json:"nodes,omitempty"` // the nodes they ran on, sorted
	// Message is the first member's, as Velero wrote it.
	Message string `json:"message"`
	// Ref is the first member: its drawer opens from the row.
	Ref     model.ObjectRef      `json:"ref"`
	Members []VeleroFailedVolume `json:"members"`
}

// VeleroFailedVolume is one failed volume backup.
type VeleroFailedVolume struct {
	Ref    model.ObjectRef `json:"ref"` // the PodVolumeBackup or DataUpload
	Backup string          `json:"backup"`
	Pod    string          `json:"pod,omitempty"` // a pod volume's pod
	Volume string          `json:"volume"`        // a pod volume's name, a data upload's PVC
	Node   string          `json:"node,omitempty"`
	// Message is status.message.
	Message string `json:"message"`
	ns      string
}

// veleroBackupLabel is the label of a backup's volume backups that names it.
const veleroBackupLabel = "velero.io/backup-name"

// failedVolumes are the failed volume backups by their backup's name, read once per model.
func (mb *veleroModeler) failedVolumes() map[string][]VeleroFailedVolume {
	if mb.failed != nil {
		return mb.failed
	}
	mb.failed = map[string][]VeleroFailedVolume{}
	for _, key := range []string{VeleroPodVolumeBackupsKey, VeleroDataUploadsKey} {
		for _, o := range mb.objs(key) {
			if model.NestedString(o.Object, "status", "phase") != phaseFailed {
				continue
			}
			v := failedVolumeOf(key, o)
			v.Ref = mb.ref(key, o)
			backup := backupNameOf(o)
			v.Backup = backup
			mb.failed[backup] = append(mb.failed[backup], v)
		}
	}
	return mb.failed
}

// failedVolumeOf reads a failed PodVolumeBackup's pod, volume and node, or a DataUpload's PVC and
// node, and its message.
func failedVolumeOf(key string, o *unstructured.Unstructured) VeleroFailedVolume {
	v := VeleroFailedVolume{Message: strings.TrimSpace(model.NestedString(o.Object, "status", "message"))}
	if key == VeleroDataUploadsKey {
		v.ns = model.NestedString(o.Object, "spec", "sourceNamespace")
		v.Volume = model.NestedString(o.Object, "spec", "sourcePVC")
		v.Node = model.NestedString(o.Object, "status", "node")
		return v
	}
	v.ns = model.NestedString(o.Object, "spec", "pod", "namespace")
	v.Pod = model.NestedString(o.Object, "spec", "pod", "name")
	v.Volume = model.NestedString(o.Object, "spec", "volume")
	v.Node = model.NestedString(o.Object, "spec", "node")
	return v
}

// backupNameOf is the backup a volume backup belongs to: its owner Backup, else its label (a label
// value is cut at 63 characters, an owner's name is not).
func backupNameOf(o *unstructured.Unstructured) string {
	for _, ref := range o.GetOwnerReferences() {
		if ref.Kind == "Backup" {
			return ref.Name
		}
	}
	return o.GetLabels()[veleroBackupLabel]
}

// volumeGroups groups the failed volume backups of the backups in Needs attention, in the page's
// namespaces: most volumes first, then by namespace.
func (mb *veleroModeler) volumeGroups() []VeleroVolumeGroup {
	failed := mb.failedVolumes()
	type group struct {
		VeleroVolumeGroup
		key string
	}
	var groups []*group
	byKey := map[string]*group{}
	seen := map[string]bool{}
	for _, a := range mb.m.Attention { // the backups the page shows in Needs attention
		if a.Ref.Kind != VeleroBackupsKey || seen[a.Ref.Name] {
			continue
		}
		seen[a.Ref.Name] = true
		for _, v := range failed[a.Ref.Name] {
			if !mb.inNamespaces(v.ns) {
				continue
			}
			key := v.ns + "\x00" + maskVolumeMessage(v.Message, v.ns, v.Pod, v.Volume)
			g := byKey[key]
			if g == nil {
				g = &group{key: key, VeleroVolumeGroup: VeleroVolumeGroup{Namespace: v.ns}}
				byKey[key] = g
				groups = append(groups, g)
			}
			g.Members = append(g.Members, v)
		}
	}
	out := make([]VeleroVolumeGroup, 0, len(groups))
	for _, g := range groups {
		slices.SortFunc(g.Members, func(a, b VeleroFailedVolume) int {
			return cmp.Or(cmp.Compare(a.Pod, b.Pod), cmp.Compare(a.Volume, b.Volume), cmp.Compare(a.Ref.Name, b.Ref.Name))
		})
		pods, nodes := map[string]struct{}{}, map[string]struct{}{}
		for _, v := range g.Members {
			if v.Pod != "" {
				pods[v.Pod] = struct{}{}
			}
			if v.Node != "" {
				nodes[v.Node] = struct{}{}
			}
		}
		g.Volumes, g.Pods = len(g.Members), len(pods)
		for n := range nodes {
			g.Nodes = append(g.Nodes, n)
		}
		slices.Sort(g.Nodes)
		g.Message, g.Ref = g.Members[0].Message, g.Members[0].Ref
		out = append(out, g.VeleroVolumeGroup)
	}
	slices.SortStableFunc(out, func(a, b VeleroVolumeGroup) int {
		return cmp.Or(cmp.Compare(b.Volumes, a.Volumes), cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Message, b.Message))
	})
	return out
}

// maskVolumeMessage is a failure's message with what is its own masked, so that volume backups that
// failed the same way read the same: its pod's name (also as namespace/pod), its volume's name, and
// every path (a word that starts with "/", a host path with the pod's UID). A word is compared
// without the punctuation around it ("pod-0:", "(data)").
func maskVolumeMessage(msg, ns, pod, volume string) string {
	words := strings.Fields(msg)
	for i, w := range words {
		core := strings.TrimRight(strings.TrimLeft(w, "\"'(["), "\"'.,:;)]")
		switch {
		case strings.HasPrefix(core, "/"):
			words[i] = "<path>"
		case pod != "" && (core == pod || core == ns+"/"+pod):
			words[i] = "<pod>"
		case volume != "" && core == volume:
			words[i] = "<volume>"
		}
	}
	return strings.Join(words, " ")
}
