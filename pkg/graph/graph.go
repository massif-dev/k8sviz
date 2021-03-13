// SPDX-FileCopyrightText: 2021 k8sviz authors
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/awalterschulze/gographviz"
	"github.com/mkimuram/k8sviz/pkg/resources"
)

func WriteDotFile(res *resources.Resources, dir, namespace, outFile string) error {
	f, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(toDot(res, dir, namespace)); err != nil {
		return err
	}

	return nil
}

func PlotDotFile(res *resources.Resources, dir, namespace, outFile, outType string) error {
	cmd := exec.Command("dot", "-T"+outType, "-o", outFile)
	cmd.Stdin = strings.NewReader(toDot(res, dir, namespace))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func toDot(res *resources.Resources, dir, namespace string) string {
	g := gographviz.NewGraph()
	g.SetDir(true)
	g.SetName("G")
	g.AddAttr("G", "rankdir", "TD")
	sname := toClusterName(namespace)
	g.AddSubGraph("G", sname,
		map[string]string{"label": toClusterLabel(dir, namespace), "labeljust": "l", "style": "dotted"})

	// Create subgraphs for resources to group by rank
	for r := 0; r < len(resources.ResourceTypes); r++ {
		g.AddSubGraph(sname, toRankName(r),
			map[string]string{"rank": "same", "style": "invis"})
		// Put dummy invisible node to order ranks
		g.AddNode(toRankName(r), toRankDummyNodeName(r),
			map[string]string{"style": "invis", "height": "0", "width": "0", "margin": "0"})
	}

	// Order ranks
	for r := 0; r < len(resources.ResourceTypes)-1; r++ {
		// Connect rth node and r+1th dummy node with invisible edge
		g.AddEdge(toRankDummyNodeName(r), toRankDummyNodeName(r+1), true,
			map[string]string{"style": "invis"})
	}

	// Put resources as Nodes in each rank of subgraph
	for r, rankRes := range resources.ResourceTypes {
		for _, resType := range strings.Fields(rankRes) {
			for _, name := range res.GetResourceNames(resType) {
				g.AddNode(toRankName(r), toResourceName(resType, name),
					map[string]string{"label": toResourceLabel(dir, resType, name), "penwidth": "0"})
			}
		}
	}

	// Connect resources
	// Owner reference for pod
	for _, pod := range res.Pods.Items {
		for _, ref := range pod.GetOwnerReferences() {
			ownerKind, err := resources.NormalizeResource(ref.Kind)
			if err != nil {
				// Skip resource that isn't available for this tool, like CRD
				continue
			}
			if !res.HasResource(ownerKind, ref.Name) {
				fmt.Fprintf(os.Stderr, "%s %s not found as a owner refernce for po %s\n", ownerKind, ref.Name, pod.Name)
				continue
			}
			g.AddEdge(toResourceName(ownerKind, ref.Name), toResourceName("pod", pod.Name), true,
				map[string]string{"style": "dashed"})
		}
	}
	// Owner reference for rs
	for _, rs := range res.Rss.Items {
		for _, ref := range rs.GetOwnerReferences() {
			ownerKind, err := resources.NormalizeResource(ref.Kind)
			if err != nil {
				// Skip resource that isn't available for this tool, like CRD
				continue
			}
			if !res.HasResource(ownerKind, ref.Name) {
				fmt.Fprintf(os.Stderr, "%s %s not found as a owner refernce for rs %s\n", ownerKind, ref.Name, rs.Name)
				continue
			}

			g.AddEdge(toResourceName(ownerKind, ref.Name), toResourceName("rs", rs.Name), true,
				map[string]string{"style": "dashed"})
		}
	}

	// pvc and pod
	for _, pod := range res.Pods.Items {
		for _, vol := range pod.Spec.Volumes {
			if vol.VolumeSource.PersistentVolumeClaim != nil {
				if !res.HasResource("pvc", vol.VolumeSource.PersistentVolumeClaim.ClaimName) {
					fmt.Fprintf(os.Stderr, "pvc %s not found as a volume for pod %s\n", vol.VolumeSource.PersistentVolumeClaim.ClaimName, pod.Name)
					continue
				}

				g.AddEdge(toResourceName("pod", pod.Name), toResourceName("pvc", vol.VolumeSource.PersistentVolumeClaim.ClaimName), true,
					map[string]string{"dir": "none"})
			}
		}
	}

	// svc and pod
	for _, svc := range res.Svcs.Items {
		if len(svc.Spec.Selector) == 0 {
			continue
		}
		// Check if pod has all labels specified in svc.Spec.Selector
		for _, pod := range res.Pods.Items {
			podLabel := pod.GetLabels()
			matched := true
			for selKey, selVal := range svc.Spec.Selector {
				val, ok := podLabel[selKey]
				if !ok || selVal != val {
					matched = false
					break
				}
			}

			if matched {
				g.AddEdge(toResourceName("pod", pod.Name), toResourceName("svc", svc.Name), true,
					map[string]string{"dir": "back"})
			}
		}
	}

	// ingress and svc
	for _, ing := range res.Ingresses.Items {
		for _, rule := range ing.Spec.Rules {
			for _, path := range rule.IngressRuleValue.HTTP.Paths {
				if !res.HasResource("svc", path.Backend.ServiceName) {
					fmt.Fprintf(os.Stderr, "svc %s not found for ingress %s\n", path.Backend.ServiceName, ing.Name)
					continue
				}

				g.AddEdge(toResourceName("svc", path.Backend.ServiceName), toResourceName("ing", ing.Name), true, map[string]string{"dir": "back"})
			}
		}
	}

	return g.String()
}

func toImagePath(dir, resource string) string {
	return filepath.Join(dir, "icons", resource+imageSuffix)
}

func escapeName(name string) string {
	return strings.NewReplacer(".", "_", "-", "_").Replace(name)
}

func toClusterName(namespace string) string {
	return clusterPrefix + escapeName(namespace)
}

func toResourceName(resType, name string) string {
	return resType + "_" + escapeName(name)
}

func toRankName(rank int) string {
	return fmt.Sprintf("%s%d", rankPrefix, rank)
}

func toRankDummyNodeName(rank int) string {
	return fmt.Sprintf("%d", rank)
}

func toClusterLabel(dir, namespace string) string {
	return toResourceLabel(dir, "ns", namespace)
}

func toResourceLabel(dir, resType, name string) string {
	return fmt.Sprintf("<<TABLE BORDER=\"0\"><TR><TD><IMG SRC=\"%s\" /></TD></TR><TR><TD>%s</TD></TR></TABLE>>", toImagePath(dir, resType), name)
}