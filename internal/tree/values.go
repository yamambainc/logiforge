package tree

// Value は、ノードの値である。true、false、null（未決）の 3 値（docs/tree/propagation.md）。
type Value int8

const (
	Null Value = iota
	True
	False
)

// Ptr は、JSON に書くための値を返す。Null は nil。
func (v Value) Ptr() *bool {
	switch v {
	case True:
		b := true
		return &b
	case False:
		b := false
		return &b
	}
	return nil
}

// Values は、木から計算したものである。保存しない（design/files/layout.md の「保存しないもの」）。
type Values struct {
	Hypothesis           map[string]Value
	Story                map[string]Value
	Leaf                 map[string]bool
	NeedsReconsideration map[string]bool
	// Evidence は、根拠部分木のノードの識別子である。根の値が決まっていなければ空。
	Evidence map[string]bool
}

// Compute は、木の値を計算する。
func Compute(t *Tree) *Values {
	v := &Values{
		Hypothesis:           map[string]Value{},
		Story:                map[string]Value{},
		Leaf:                 map[string]bool{},
		NeedsReconsideration: map[string]bool{},
		Evidence:             map[string]bool{},
	}
	for _, r := range t.Roots {
		v.hypothesis(r)
	}
	if root := t.Root(); root != nil && v.Hypothesis[root.ID] != Null {
		v.evidence(root)
	}
	return v
}

func (v *Values) hypothesis(h *Hypothesis) Value {
	for _, s := range h.Stories {
		v.story(s)
	}
	active := h.ActiveStories()
	v.Leaf[h.ID] = len(active) == 0
	var val Value
	if len(active) == 0 {
		valid := h.ValidVerdicts()
		if len(valid) == 1 {
			if valid[0].Value {
				val = True
			} else {
				val = False
			}
		}
	} else {
		// active な子ストーリーの OR。ただし false は伝わらない。
		allFalse := true
		for _, s := range active {
			sv := v.Story[s.ID]
			if sv == True {
				val = True
			}
			if sv != False {
				allFalse = false
			}
		}
		v.NeedsReconsideration[h.ID] = allFalse
	}
	v.Hypothesis[h.ID] = val
	return val
}

func (v *Values) story(s *Story) Value {
	// 子仮説の AND（Kleene）。
	val := True
	for _, h := range s.Hypotheses {
		switch v.hypothesis(h) {
		case False:
			val = False
		case Null:
			if val != False {
				val = Null
			}
		}
	}
	if len(s.Hypotheses) == 0 {
		val = Null
	}
	v.Story[s.ID] = val
	return val
}

func (v *Values) evidence(h *Hypothesis) {
	v.Evidence[h.ID] = true
	if v.Leaf[h.ID] {
		return
	}
	for _, s := range h.ActiveStories() {
		if v.Story[s.ID] != True {
			continue
		}
		v.Evidence[s.ID] = true
		for _, c := range s.Hypotheses {
			v.evidence(c)
		}
	}
}
