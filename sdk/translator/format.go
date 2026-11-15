package translator

type Format string

func FromString(v string) Format {
	return Format(v)
}

func (f Format) String() string {
	return string(f)
}
