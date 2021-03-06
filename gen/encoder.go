package gen

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/mailru/easyjson"
)

func (g *Generator) getStructEncoderName(t reflect.Type) string {
	return g.functionName("easyjson_encode_", t)
}

var primitiveEncoders = map[reflect.Kind]string{
	reflect.String:  "out.String",
	reflect.Bool:    "out.Bool",
	reflect.Int:     "out.Int",
	reflect.Int8:    "out.Int8",
	reflect.Int16:   "out.Int16",
	reflect.Int32:   "out.Int32",
	reflect.Int64:   "out.Int64",
	reflect.Uint:    "out.Uint",
	reflect.Uint8:   "out.Uint8",
	reflect.Uint16:  "out.Uint16",
	reflect.Uint32:  "out.Uint32",
	reflect.Uint64:  "out.Uint64",
	reflect.Float32: "out.Float32",
	reflect.Float64: "out.Float64",
}

func (g *Generator) genTypeEncoder(t reflect.Type, in string, indent int) error {
	ws := strings.Repeat("  ", indent)

	marshalerIface := reflect.TypeOf((*easyjson.Marshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(marshalerIface) {
		fmt.Fprintln(g.out, ws+"("+in+").MarshalEasyJSON(out)")
		return nil
	}

	marshalerIface = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(marshalerIface) {
		fmt.Fprintln(g.out, ws+"out.Raw( ("+in+").MarshalJSON() )")
		return nil
	}

	// Check whether type is primitive, needs to be done after interface check.
	if enc := primitiveEncoders[t.Kind()]; enc != "" {
		fmt.Fprintln(g.out, ws+enc+"("+in+")")
		return nil
	}

	switch t.Kind() {
	case reflect.Slice:
		elem := t.Elem()
		iVar := g.uniqueVarName()
		vVar := g.uniqueVarName()

		fmt.Fprintln(g.out, ws+"out.RawByte('[')")
		fmt.Fprintln(g.out, ws+"for "+iVar+", "+vVar+" := range "+in+" {")
		fmt.Fprintln(g.out, ws+"  if "+iVar+" > 0 {")
		fmt.Fprintln(g.out, ws+"    out.RawByte(',')")
		fmt.Fprintln(g.out, ws+"  }")

		g.genTypeEncoder(elem, vVar, indent+1)

		fmt.Fprintln(g.out, ws+"}")
		fmt.Fprintln(g.out, ws+"out.RawByte(']')")

	case reflect.Struct:
		enc := g.getStructEncoderName(t)
		g.addType(t)

		fmt.Fprintln(g.out, ws+enc+"(out, &"+in+")")

	case reflect.Ptr:
		fmt.Fprintln(g.out, ws+"if "+in+" == nil {")
		fmt.Fprintln(g.out, ws+`  out.RawString("null")`)
		fmt.Fprintln(g.out, ws+"} else {")

		g.genTypeEncoder(t.Elem(), "*"+in, indent+1)

		fmt.Fprintln(g.out, ws+"}")

	case reflect.Map:
		key := t.Key()
		if key.Kind() != reflect.String {
			return fmt.Errorf("map type %v not supported: only string keys are allowed", key)
		}
		tmpVar := g.uniqueVarName()

		fmt.Fprintln(g.out, ws+"out.RawByte('{')")
		fmt.Fprintln(g.out, ws+tmpVar+"_first := true")
		fmt.Fprintln(g.out, ws+"for "+tmpVar+"_name, "+tmpVar+"_value := range "+in+" {")
		fmt.Fprintln(g.out, ws+"  if !"+tmpVar+"_first { out.RawByte(',') }")
		fmt.Fprintln(g.out, ws+"  "+tmpVar+"_first = false")
		fmt.Fprintln(g.out, ws+"  out.String("+tmpVar+"_name)")

		g.genTypeEncoder(t.Elem(), tmpVar+"_value", indent+1)

		fmt.Fprintln(g.out, ws+"}")
		fmt.Fprintln(g.out, ws+"out.RawByte('}')")

	case reflect.Interface:
		if t.NumMethod() != 0 {
			return fmt.Errorf("interface type %v not supported: only interface{} is allowed", t)
		}
		fmt.Fprintln(g.out, ws+"out.Raw(json.Marshal("+in+"))")

	default:
		return fmt.Errorf("don't know how to encode %v", t)
	}
	return nil
}

func (g *Generator) notEmptyCheck(t reflect.Type, v string) string {
	optionalIface := reflect.TypeOf((*easyjson.Optional)(nil)).Elem()
	if reflect.PtrTo(t).Implements(optionalIface) {
		return "(" + v + ").IsDefined()"
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Map:
		return "len(" + v + ") != 0"
	case reflect.Interface, reflect.Ptr:
		return v + " != nil"
	case reflect.Bool:
		return v
	case reflect.String:
		return v + ` != ""`
	case reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:

		return v + " != 0"

	default:
		return "true"
	}
}

func (g *Generator) genStructFieldEncoder(t reflect.Type, f reflect.StructField) error {
	jsonName := g.namer.GetJSONFieldName(t, f)
	omitEmpty := g.omitEmpty

	for i, s := range strings.Split(f.Tag.Get("json"), ",") {
		if i > 0 && s == "omitempty" {
			omitEmpty = true
		} else if i > 0 && s == "!omitempty" {
			omitEmpty = false
		}
	}

	if !omitEmpty {
		fmt.Fprintln(g.out, "  if !first { out.RawByte(',') }")
		fmt.Fprintln(g.out, "  first = false")
		fmt.Fprintf(g.out, "  out.RawString(%q)\n", strconv.Quote(jsonName)+":")
		return g.genTypeEncoder(f.Type, "in."+f.Name, 1)
	}

	fmt.Fprintln(g.out, "  if", g.notEmptyCheck(f.Type, "in."+f.Name), "{")
	fmt.Fprintln(g.out, "    if !first { out.RawByte(',') }")
	fmt.Fprintln(g.out, "    first = false")

	fmt.Fprintf(g.out, "    out.RawString(%q)\n", strconv.Quote(jsonName)+":")
	if err := g.genTypeEncoder(f.Type, "in."+f.Name, 2); err != nil {
		return err
	}
	fmt.Fprintln(g.out, "  }")
	return nil
}

func (g *Generator) genStructEncoder(t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a struct type", t)
	}

	fname := g.getStructEncoderName(t)
	typ := g.getType(t)

	fmt.Fprintln(g.out, "func "+fname+"(out *jwriter.Writer, in *"+typ+") {")
	fmt.Fprintln(g.out, "  out.RawByte('{')")
	fmt.Fprintln(g.out, "  first := true")
	fmt.Fprintln(g.out, "  _ = first")

	for _, f := range getStructFields(t) {
		if err := g.genStructFieldEncoder(t, f); err != nil {
			return err
		}
	}

	fmt.Fprintln(g.out, "  out.RawByte('}')")
	fmt.Fprintln(g.out, "}")

	return nil
}

func (g *Generator) genStructMarshaller(t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a struct type", t)
	}

	fname := g.getStructEncoderName(t)
	typ := g.getType(t)

	if !g.noStdMarshalers {
		fmt.Fprintln(g.out, "func (v *"+typ+") MarshalJSON() ([]byte, error) {")
		fmt.Fprintln(g.out, "  w := jwriter.Writer{}")
		fmt.Fprintln(g.out, "  "+fname+"(&w, v)")
		fmt.Fprintln(g.out, "  return w.Buffer.BuildBytes(), w.Error")
		fmt.Fprintln(g.out, "}")
	}

	fmt.Fprintln(g.out, "func (v *"+typ+") MarshalEasyJSON(w *jwriter.Writer) {")
	fmt.Fprintln(g.out, "  "+fname+"(w, v)")
	fmt.Fprintln(g.out, "}")

	return nil
}
