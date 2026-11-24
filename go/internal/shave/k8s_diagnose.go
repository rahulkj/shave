package shave

import (
	"flag"
	"fmt"
	"strings"
)

// cmdK8sDiagnose is the implementation behind `shave k8s diagnose`.
func cmdK8sDiagnose(args []string) int {
	fs := flag.NewFlagSet("k8s diagnose", flag.ContinueOnError)
	ns := fs.String("n", "", "namespace")
	jsonOut := fs.Bool("json", false, "json output")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: shave k8s diagnose <kind/name> [-n ns] [--json]")
		return 2
	}
	target := fs.Arg(0)
	parts := strings.SplitN(target, "/", 2)
	kind, name := target, ""
	if len(parts) == 2 {
		kind, name = parts[0], parts[1]
	} else {
		kind = "pod"
		name = target
	}

	kn := []string{}
	if *ns != "" {
		kn = append(kn, "-n", *ns)
	}

	// describe
	descLines := []string{}
	dargs := append([]string{"describe", kind, name}, kn...)
	desc, _, rc := runKubectl(dargs...)
	if rc != 0 {
		descLines = append(descLines, strings.TrimSpace(desc))
	} else {
		sc := 0
		for _, line := range strings.Split(desc, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			descLines = append(descLines, trunc(redactLine(line), 300))
			sc++
			if sc >= 80 {
				descLines = append(descLines, "... (describe truncated)")
				break
			}
		}
	}

	// events
	eventLines := []string{}
	eout, _, _ := runKubectl(append([]string{"get", "events", "--field-selector=involvedObject.name=" + name}, kn...)...)
	for i, l := range strings.Split(eout, "\n") {
		if i == 0 || strings.TrimSpace(l) == "" {
			continue
		}
		eventLines = append(eventLines, trunc(l, 300))
		if len(eventLines) >= 30 {
			eventLines = append(eventLines, "... (events truncated)")
			break
		}
	}

	// logs for pods
	logLines := []string{}
	if strings.EqualFold(kind, "pod") {
		lout, lerr, _ := runKubectl(append([]string{"logs", name, "--tail=50"}, kn...)...)
		if lerr != "" {
			logLines = append(logLines, "kubectl logs: "+strings.TrimSpace(lerr))
		} else {
			for _, line := range strings.Split(lout, "\n") {
				logLines = append(logLines, trunc(redactLine(line), 500))
			}
		}
	}

	if *jsonOut {
		writeJSON(map[string]interface{}{
			"kind":     kind,
			"name":     name,
			"describe": descLines,
			"events":   eventLines,
			"logs":     logLines,
		})
		return 0
	}

	fmt.Printf("== %s %s ==\n", kind, name)
	fmt.Println("--- describe ---")
	for _, l := range descLines {
		fmt.Println(l)
	}
	fmt.Println("--- events ---")
	if len(eventLines) == 0 {
		fmt.Println("(none)")
	} else {
		for _, l := range eventLines {
			fmt.Println(l)
		}
	}
	if strings.EqualFold(kind, "pod") {
		fmt.Println("--- logs (tail 50) ---")
		for _, l := range logLines {
			fmt.Println(l)
		}
	}
	return 0
}
