package problems

// The AI edit guard (v2.1.0 stage A): a model that rewrites problem.yaml by a
// key list drops the keys it was not told about, such as `generator`. Every
// top-level key the current problem.yaml has and the reply's lacks is put back
// from the current one, so an edit cannot remove a setting by leaving it out.

import (
	"bytes"
	"reflect"
	"slices"

	"go.yaml.in/yaml/v3"
)

// EditPolicyError is a refusal that asking again cannot fix: test data in the
// reply, a changed slug, too many files, a file that is not a pack file, no
// change at all, or a changed mode. ModeChange marks the last one, which has
// its own code and copy.
type EditPolicyError struct {
	Problems   []string
	ModeChange bool
}

func (e *EditPolicyError) Error() string { return e.Problems[0] }

// keptLabels is what a draft calls the keys it put back, in the order it
// lists them. A key not named here counts as "other settings".
var keptLabels = []string{
	"the test generator", "the generator settings of each subtask", "the input validator", "the brute force",
	"the checker", "the samples", "the limits", "the starting code", "the locked code", "other settings",
}

func keptLabel(key string) string {
	switch key {
	case "generator":
		return keptLabels[0]
	case "generator_args":
		return keptLabels[1]
	case "validator":
		return keptLabels[2]
	case "brute":
		return keptLabels[3]
	case "checker", "epsilon":
		return keptLabels[4]
	case "samples", "open":
		return keptLabels[5]
	case "time_limit_ms", "memory_limit_mb":
		return keptLabels[6]
	case "starters":
		return keptLabels[7]
	case "locked_top", "locked_bottom":
		return keptLabels[8]
	}
	return keptLabels[9]
}

// namesFile lists the keys whose value is a file name, or a list of them.
var namesFile = []string{"reference", "brute", "validator", "generator", "starters", "locked_top", "locked_bottom"}

// replyDealtWith reports whether the reply sent the file a key names or, for
// the generator keys, tests.yaml: then the model dealt with the key itself and
// the parser judges the reply as sent.
func replyDealtWith(key string, value *yaml.Node, sent map[string][]byte) bool {
	if _, ok := sent[packPlanFile]; ok && (key == "generator" || key == "generator_args") {
		return true
	}
	if !slices.Contains(namesFile, key) {
		return false
	}
	switch value.Kind {
	case yaml.ScalarNode:
		_, ok := sent[value.Value]
		return ok
	case yaml.SequenceNode:
		for _, n := range value.Content {
			if _, ok := sent[n.Value]; ok {
				return true
			}
		}
	}
	return false
}

func mappingOf(raw []byte) (*yaml.Node, *yaml.Node) {
	var doc yaml.Node
	if yaml.Unmarshal(raw, &doc) != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	return &doc, doc.Content[0]
}

// restoreOmitted puts into reply, a problem.yaml, every top-level key the
// current problem.yaml has and reply lacks, and each subtask's generator_args
// when the subtask count is unchanged, except what replyDealtWith excludes.
// sent holds the files the reply carried. It returns the new text and the
// labels of what was put back, in the order of keptLabels; text that is not a
// YAML mapping comes back unchanged for the parser to refuse.
func restoreOmitted(current, reply []byte, sent map[string][]byte) ([]byte, []string) {
	_, cur := mappingOf(current)
	doc, rep := mappingOf(reply)
	if cur == nil || rep == nil {
		return reply, nil
	}
	var keys []string
	for i := 0; i+1 < len(cur.Content); i += 2 {
		k, v := cur.Content[i], cur.Content[i+1]
		if mapValue(rep, k.Value) == nil && !replyDealtWith(k.Value, v, sent) {
			rep.Content = append(rep.Content, k, v)
			keys = append(keys, k.Value)
		}
	}
	if curSub, repSub := mapValue(cur, "subtasks"), mapValue(rep, "subtasks"); curSub != nil && repSub != nil &&
		curSub.Kind == yaml.SequenceNode && repSub.Kind == yaml.SequenceNode && len(curSub.Content) == len(repSub.Content) {
		for i, st := range curSub.Content {
			rs := repSub.Content[i]
			if st.Kind != yaml.MappingNode || rs.Kind != yaml.MappingNode {
				continue
			}
			if args := mapValue(st, "generator_args"); args != nil && mapValue(rs, "generator_args") == nil && !replyDealtWith("generator_args", args, sent) {
				rs.Content = append(rs.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "generator_args"}, args)
				keys = append(keys, "generator_args")
			}
		}
	}
	if len(keys) == 0 {
		return reply, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if enc.Encode(doc) != nil || enc.Close() != nil {
		return reply, nil
	}
	var kept []string
	for _, label := range keptLabels {
		for _, k := range keys {
			if keptLabel(k) == label {
				kept = append(kept, label)
				break
			}
		}
	}
	return buf.Bytes(), kept
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// sameYAML reports whether two problem.yaml texts say the same thing.
func sameYAML(a, b []byte) bool {
	var x, y any
	return yaml.Unmarshal(a, &x) == nil && yaml.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// editMode is the mode a problem.yaml states, "whole" when it states none; ok
// is false when the text cannot be read.
func editMode(raw []byte) (mode string, ok bool) {
	var head struct {
		Mode string `yaml:"mode"`
	}
	if yaml.Unmarshal(raw, &head) != nil {
		return "", false
	}
	if head.Mode == ModeFunction {
		return ModeFunction, true
	}
	return ModeWhole, true
}
