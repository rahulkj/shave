package shave

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func runKubectl(args ...string) (string, string, int) {
	return runCmd("kubectl", args...)
}

type k8sPodList struct {
	Items []struct {
		Metadata struct {
			Name              string `json:"name"`
			Namespace         string `json:"namespace"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
		Status struct {
			Phase             string `json:"phase"`
			Reason            string `json:"reason"`
			ContainerStatuses []struct {
				Ready        bool `json:"ready"`
				RestartCount int  `json:"restartCount"`
				State        struct {
					Waiting struct {
						Reason string `json:"reason"`
					} `json:"waiting"`
					Terminated struct {
						Reason   string `json:"reason"`
						ExitCode int    `json:"exitCode"`
					} `json:"terminated"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func cmdK8s(args []string) int {
	if len(args) == 0 {
		fmt.Println("usage: shave k8s [pods|events|diagnose] ...")
		return 2
	}
	switch args[0] {
	case "pods":
		return cmdK8sPods(args[1:])
	case "events":
		return cmdK8sEvents(args[1:])
	case "diagnose":
		return cmdK8sDiagnose(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "shave: unknown k8s subcommand %s\n", args[0])
		return 2
	}
}

func nsFlag(fs *flag.FlagSet, ns *string, all *bool) {
	fs.StringVar(ns, "n", "", "namespace")
	fs.StringVar(ns, "namespace", "", "namespace")
	fs.BoolVar(all, "A", false, "all namespaces")
	fs.BoolVar(all, "all-namespaces", false, "all namespaces")
}

func cmdK8sPods(args []string) int {
	fs := flag.NewFlagSet("k8s pods", flag.ContinueOnError)
	ns := fs.String("n", "", "namespace")
	all := fs.Bool("A", false, "all namespaces")
	jsonOut := fs.Bool("json", false, "json output")
	_ = fs.Parse(args)
	// allow overrides
	_ = ns
	_ = all
	// re-parse with aliases handled below by reading flags already

	kargs := []string{"get", "pods", "-o", "json"}
	if *all {
		kargs = append(kargs, "-A")
	} else if *ns != "" {
		kargs = append(kargs, "-n", *ns)
	}
	out, estr, rc := runKubectl(kargs...)
	if rc != 0 {
		fmt.Fprintln(os.Stderr, "kubectl:", estr)
		return 1
	}
	var pl k8sPodList
	if err := json.Unmarshal([]byte(out), &pl); err != nil {
		fmt.Fprintln(os.Stderr, "shave: parse:", err)
		return 1
	}

	type row struct {
		NS       string `json:"namespace"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Ready    string `json:"ready"`
		Restarts int    `json:"restarts"`
		Age      string `json:"age"`
		Reason   string `json:"reason,omitempty"`
	}
	var rows []row
	now := time.Now()
	for _, p := range pl.Items {
		ready := 0
		total := len(p.Status.ContainerStatuses)
		restarts := 0
		reason := p.Status.Reason
		for _, c := range p.Status.ContainerStatuses {
			if c.Ready {
				ready++
			}
			restarts += c.RestartCount
			if reason == "" {
				if c.State.Waiting.Reason != "" {
					reason = c.State.Waiting.Reason
				} else if c.State.Terminated.Reason != "" {
					reason = c.State.Terminated.Reason
				}
			}
		}
		status := p.Status.Phase
		if reason != "" && (status == "Failed" || status == "Pending" || status == "Unknown") {
			status = status + ":" + reason
		}
		readyStr := fmt.Sprintf("%d/%d", ready, total)
		if total == 0 {
			readyStr = "-"
		}
		age := ""
		if t, err := time.Parse(time.RFC3339, p.Metadata.CreationTimestamp); err == nil {
			age = humanAge(now.Sub(t))
		}
		rows = append(rows, row{NS: p.Metadata.Namespace, Name: p.Metadata.Name, Status: status, Ready: readyStr, Restarts: restarts, Age: age, Reason: reason})
	}

	if *jsonOut {
		writeJSON(map[string]interface{}{"pods": rows, "count": len(rows)})
		return 0
	}
	// table
	if len(rows) == 0 {
		fmt.Println("(no pods)")
		return 0
	}
	// width
	fmt.Printf("%-24s %-34s %-14s %-7s %-8s %s\n", "NAMESPACE", "NAME", "STATUS", "READY", "RESTARTS", "AGE")
	for _, r := range rows {
		name := r.Name
		if len(name) > 33 {
			name = name[:30] + "…"
		}
		fmt.Printf("%-24s %-34s %-14s %-7s %-8d %s\n", trim(r.NS, 23), name, trim(r.Status, 13), r.Ready, r.Restarts, r.Age)
	}
	return 0
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func humanAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func cmdK8sEvents(args []string) int {
	fs := flag.NewFlagSet("k8s events", flag.ContinueOnError)
	ns := fs.String("n", "", "namespace")
	all := fs.Bool("A", false, "all namespaces")
	jsonOut := fs.Bool("json", false, "json output")
	max := fs.Int("max", 40, "max events")
	_ = fs.Parse(args)

	kargs := []string{"get", "events", "-o", "json", "--sort-by=.lastTimestamp"}
	if *all {
		kargs = append(kargs, "-A")
	} else if *ns != "" {
		kargs = append(kargs, "-n", *ns)
	}
	out, estr, rc := runKubectl(kargs...)
	if rc != 0 {
		fmt.Fprintln(os.Stderr, "kubectl:", estr)
		return 1
	}
	var el struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			InvolvedObject struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"involvedObject"`
			Type          string `json:"type"`
			Reason        string `json:"reason"`
			Message       string `json:"message"`
			Count         int    `json:"count"`
			LastTimestamp string `json:"lastTimestamp"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &el); err != nil {
		fmt.Fprintln(os.Stderr, "shave: parse:", err)
		return 1
	}
	// dedup by object+reason+message
	type key struct{ a, b, c string }
	seen := map[key]int{}
	var ents []struct {
		NS     string
		Obj    string
		Type   string
		Reason string
		Msg    string
		Count  int
		When   string
	}
	for _, e := range el.Items {
		k := key{e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name, e.Reason, e.Message}
		if idx, ok := seen[k]; ok {
			ents[idx].Count += e.Count
			if e.Count > 0 {
				// keep latest timestamp
			}
			_ = idx
			continue
		}
		seen[k] = len(ents)
		obj := e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name
		msg := e.Message
		if len(msg) > 160 {
			msg = msg[:157] + "…"
		}
		ents = append(ents, struct {
			NS     string
			Obj    string
			Type   string
			Reason string
			Msg    string
			Count  int
			When   string
		}{NS: e.Metadata.Namespace, Obj: obj, Type: e.Type, Reason: e.Reason, Msg: msg, Count: e.Count, When: e.LastTimestamp})
	}
	if len(ents) == 0 {
		fmt.Println("(no events)")
		return 0
	}
	// limit tail
	if len(ents) > *max {
		ents = ents[len(ents)-*max:]
	}
	if *jsonOut {
		writeJSON(ents)
		return 0
	}
	fmt.Printf("%-8s %-28s %-26s %s\n", "TYPE", "OBJECT", "REASON", "MESSAGE")
	for _, e := range ents {
		fmt.Printf("%-8s %-28s %-26s %s", trim(e.Type, 7), trim(e.Obj, 27), trim(e.Reason, 25), e.Msg)
		if e.Count > 1 {
			fmt.Printf(" (x%d)", e.Count)
		}
		fmt.Println()
	}
	return 0
}
