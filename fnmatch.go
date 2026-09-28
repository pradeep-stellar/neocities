package neocities

// fnmatch reports whether name matches pattern using the same rules as Ruby's
// File.fnmatch? without FNM_PATHNAME or FNM_DOTMATCH: '*' matches across
// slashes, and a leading '.' is matched only by a literal '.' in the pattern.
func fnmatch(pattern, name string) bool {
	return matchPattern([]byte(pattern), []byte(name), true)
}

func matchPattern(pat, s []byte, atStart bool) bool {
	if atStart && len(s) > 0 && s[0] == '.' && !patternStartsWithDot(pat) {
		return false
	}
	for len(pat) > 0 {
		switch pat[0] {
		case '*':
			for len(pat) > 0 && pat[0] == '*' {
				pat = pat[1:]
			}
			if len(pat) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if matchPattern(pat, s[i:], false) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			pat, s = pat[1:], s[1:]
		case '[':
			end := classEnd(pat)
			if end < 0 {
				if len(s) == 0 || s[0] != '[' {
					return false
				}
				pat, s = pat[1:], s[1:]
				continue
			}
			if len(s) == 0 || !matchClass(pat[1:end], s[0]) {
				return false
			}
			pat, s = pat[end+1:], s[1:]
		case '\\':
			if len(pat) < 2 || len(s) == 0 || s[0] != pat[1] {
				return false
			}
			pat, s = pat[2:], s[1:]
		default:
			if len(s) == 0 || s[0] != pat[0] {
				return false
			}
			pat, s = pat[1:], s[1:]
		}
	}
	return len(s) == 0
}

func patternStartsWithDot(pat []byte) bool {
	if len(pat) == 0 {
		return false
	}
	if pat[0] == '\\' {
		return len(pat) > 1 && pat[1] == '.'
	}
	return pat[0] == '.'
}

func classEnd(pat []byte) int {
	if len(pat) < 2 || pat[0] != '[' {
		return -1
	}
	i := 1
	if pat[i] == '!' || pat[i] == '^' {
		i++
	}
	for ; i < len(pat); i++ {
		if pat[i] == '\\' && i+1 < len(pat) {
			i++
			continue
		}
		if pat[i] == ']' && i > 1 {
			return i
		}
	}
	return -1
}

func matchClass(class []byte, c byte) bool {
	if len(class) == 0 {
		return false
	}
	negate := class[0] == '!' || class[0] == '^'
	if negate {
		class = class[1:]
	}
	matched := false
	for i := 0; i < len(class); i++ {
		if class[i] == '\\' && i+1 < len(class) {
			i++
			if class[i] == c {
				matched = true
				break
			}
			continue
		}
		if i+2 < len(class) && class[i+1] == '-' {
			start, endc := class[i], class[i+2]
			if start > endc {
				start, endc = endc, start
			}
			if c >= start && c <= endc {
				matched = true
				break
			}
			i += 2
			continue
		}
		if class[i] == c {
			matched = true
			break
		}
	}
	if negate {
		return !matched
	}
	return matched
}
