package deps

func Resolve(profile string) Versions {
	v, ok := Registry[profile]
	if !ok {
		return Registry["stable"]
	}
	return v
}
