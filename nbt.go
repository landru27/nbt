package nbt

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//  import necessary external packages  ///////////////////////////////////////////////////////////////////////////////////////

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//  NBT datatypes  ////////////////////////////////////////////////////////////////////////////////////////////////////////////
//
// NBT, Named Binary Tag, is the hierarchical data structure that Minecraft uses to store most game data
//
// reference : http://minecraft.gamepedia.com/NBT_format
//
// TAG_End items are stored as the single byte indicating the type as TAG_End; other TAG types are followed by a name-length
// and a name, although the name can be empty (so, the length can be zero); _Array types are followed by the number of
// elements in the array and that number of elements of the appropriate size; TAG_List is followed by a TAG type byte, the
// number of List elements, and then that number of unnamed TAG items of that type (so, without repeating the type and without
// a name-length and without a name string); TAG_Compound is followed by fully-formed TAG items (including nested TAG_List and
// TAG_Compound items) until ended by a TAG_End item
//
// TAG types other than TAG_Compound are of the size dictated by either datatype-size or encoded collection (Array, List) size,
// and so are followed immediately by the next item; TAG_Compound items are ended by a TAG_End item, because the size varies
// by the variability of the items that might follow it
//
// a TAG_List can be empty, in which case the type is TAG_End and the size is zero, with no further bytes for the List item

type NBTTAG byte

const (
	TAG_End        NBTTAG = iota //  0 : size: 0                 no payload, no name
	TAG_Byte                     //  1 : size: 1                 signed  8-bit integer
	TAG_Short                    //  2 : size: 2                 signed 16-bit integer
	TAG_Int                      //  3 : size: 4                 signed 32-bit integer
	TAG_Long                     //  4 : size: 8                 signed 64-bit integer
	TAG_Float                    //  5 : size: 4                 IEEE 754-2008 32-bit floating point number
	TAG_Double                   //  6 : size: 8                 IEEE 754-2008 64-bit floating point number
	TAG_Byte_Array               //  7 : size: 4 + 1*elem        size TAG_Int, then payload [size]byte
	TAG_String                   //  8 : size: 2 + 4*elem        length TAG_Short, then payload (utf-8) string (of length length)
	TAG_List                     //  9 : size: 1 + 4 + len*elem  tagID TAG_Byte, length TAG_Int, then payload [length]tagID
	TAG_Compound                 // 10 : size: varies            { tagID TAG_Byte, name TAG_String, payload tagID }... TAG_End
	TAG_Int_Array                // 11 : size: 4 + 4*elem        size TAG_Int, then payload [size]TAG_Int
	TAG_Long_Array               // 12 : size: 4 + 8*elem        size TAG_Int, then payload [size]TAG_Long
	TAG_NULL                     // 13 : local extension of the NBT spec, for indicating 'not yet known', or 'read data to determine', etc.
)

var NBTTAGName = map[NBTTAG]string{
	TAG_End:        "TAG_End",
	TAG_Byte:       "TAG_Byte",
	TAG_Short:      "TAG_Short",
	TAG_Int:        "TAG_Int",
	TAG_Long:       "TAG_Long",
	TAG_Float:      "TAG_Float",
	TAG_Double:     "TAG_Double",
	TAG_Byte_Array: "TAG_Byte_Array",
	TAG_String:     "TAG_String",
	TAG_List:       "TAG_List",
	TAG_Compound:   "TAG_Compound",
	TAG_Int_Array:  "TAG_Int_Array",
	TAG_Long_Array: "TAG_Long_Array",
	TAG_NULL:       "TAG_NULL",
}

func (tag NBTTAG) String() string {
	name := "Unknown"

	switch tag {
	case TAG_End:
		name = "TAG_End"
	case TAG_Byte:
		name = "TAG_Byte"
	case TAG_Short:
		name = "TAG_Short"
	case TAG_Int:
		name = "TAG_Int"
	case TAG_Long:
		name = "TAG_Long"
	case TAG_Float:
		name = "TAG_Float"
	case TAG_Double:
		name = "TAG_Double"
	case TAG_Byte_Array:
		name = "TAG_Byte_Array"
	case TAG_String:
		name = "TAG_String"
	case TAG_List:
		name = "TAG_List"
	case TAG_Compound:
		name = "TAG_Compound"
	case TAG_Int_Array:
		name = "TAG_Int_Array"
	case TAG_Long_Array:
		name = "TAG_Long_Array"
	case TAG_NULL:
		name = "TAG_NULL"
	}

	return fmt.Sprintf("%s (0x%02x)", name, byte(tag))
}

// because NBT elements are of various datatypes, the Data property is an interface{}; this also supports the hierarchical
// nature of NBT, because it allows the Data property to itself be an NBT element
//
type NBT struct {
	Type NBTTAG
	List NBTTAG
	Name string
	Size uint32
	Data interface{}
}

// because the NBT datatype has an interface{} element, and because of the extensive use of arrays / slices for that element
// in order to model the hierarchy of nested NBT data, there are many occassions where a deep copy of an existing NBT object
// is warranted
//
func (src *NBT) DeepCopy() (rslt *NBT, err error) {
	err = nil

	dst := NBT{}

	dst.Type = src.Type
	dst.List = src.List
	dst.Name = src.Name
	dst.Size = src.Size

	rt := reflect.TypeOf(src.Data)
	rk := rt.Kind()

	if (rk == reflect.Array) || (rk == reflect.Slice) {
		listnbt := make([]NBT, len(src.Data.([]NBT)))

		for indx, elem := range src.Data.([]NBT) {
			nxt, _ := elem.DeepCopy()
			listnbt[indx] = *nxt
		}

		dst.Data = listnbt
	} else {
		switch src.Type {
		case TAG_End:
			dst.Data = nil

		case TAG_Byte:
			dst.Data = src.Data.(byte)
		case TAG_Short:
			dst.Data = src.Data.(int16)
		case TAG_Int:
			dst.Data = src.Data.(int32)
		case TAG_Long:
			dst.Data = src.Data.(int64)
		case TAG_Float:
			dst.Data = src.Data.(float32)
		case TAG_Double:
			dst.Data = src.Data.(float64)
		case TAG_String:
			dst.Data = src.Data.(string)

		default:
			dst.Data = nil
		}
	}

	rslt = &dst

	return
}

// the ordinary UnmarshalJSON function works just fine for our NBT datatype, but having our own UnmarshalJSON function allows
// us to assert specific a datatype for those points where we know what specific type of data it is, i.e., the non-nested types
//
// we also use NewDecoder / UseNumber / Decode instead of Unmarshal, so that we can perform more specific conversions, instead
// of having the json library read every number as a float64; this is crucial for long ints, which Minecraft uses for seeds,
// UUIDs, and a few other things;  particularly with seeds and UUIDs, the loss of precision from a float64 conversion is fatal
//
func (nbt *NBT) UnmarshalJSON(b []byte) (err error) {
	var n interface{}
	var ns string
	var ni int64
	var nu uint64
	var nf float64

	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()

	if err := d.Decode(&n); err == nil {
		m := n.(map[string]interface{})

		rt := reflect.TypeOf(m["Data"])
		rk := rt.Kind()

		t := NBT{}

		ns = string(m["Type"].(json.Number))
		ni, _ = strconv.ParseInt(ns, 10, 8)
		t.Type = NBTTAG(byte(ni))

		ns = string(m["List"].(json.Number))
		ni, _ = strconv.ParseInt(ns, 10, 8)
		t.List = NBTTAG(byte(ni))

		t.Name = m["Name"].(string)

		ns = string(m["Size"].(json.Number))
		nu, _ = strconv.ParseUint(ns, 10, 32)
		t.Size = uint32(nu)

		if (rk == reflect.Array) || (rk == reflect.Slice) {
			p, e := json.Marshal(m["Data"])
			if e != nil {
				return e
			}

			var q []NBT
			f := json.NewDecoder(bytes.NewReader(p))
			f.UseNumber()
			err = f.Decode(&q)

			t.Data = q
		} else {
			switch t.Type {
			case TAG_End:
				return nil

			case TAG_Byte:
				ns = string(m["Data"].(json.Number))
				ni, _ = strconv.ParseInt(ns, 10, 8)
				t.Data = byte(ni)
			case TAG_Short:
				ns = string(m["Data"].(json.Number))
				ni, _ = strconv.ParseInt(ns, 10, 16)
				t.Data = int16(ni)
			case TAG_Int:
				ns = string(m["Data"].(json.Number))
				ni, _ = strconv.ParseInt(ns, 10, 32)
				t.Data = int32(ni)
			case TAG_Long:
				ns = string(m["Data"].(json.Number))
				ni, _ = strconv.ParseInt(ns, 10, 64)
				t.Data = ni
			case TAG_Float:
				ns = string(m["Data"].(json.Number))
				nf, _ = strconv.ParseFloat(ns, 32)
				t.Data = float32(nf)
			case TAG_Double:
				ns = string(m["Data"].(json.Number))
				nf, _ = strconv.ParseFloat(ns, 64)
				t.Data = nf
			case TAG_String:
				t.Data = m["Data"].(string)

			default:
				return nil
			}
		}

		*nbt = t
	}

	return
}

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// define library functions
//
func ReadNBTData(r *bytes.Reader, t NBTTAG, debug string) (rtrn NBT, err error) {
	var tb byte
	var tt NBTTAG

	// 't' is essentially a sentinal value for reading / parsing TAG_List data; if we don't already know what type of
	// NBT item we are reading, start by reading the type from the input data; if we do know (if it's been passed in as
	// part of the function call), it means we aren't going to find it in the data (chiefly (only?), because we are
	// reading the elements of a TAG_List item
	//
	if t == TAG_NULL {
		tb, err = r.ReadByte()
		if err != nil {
			return rtrn, err
		}
		tt = NBTTAG(tb)
	} else {
		tt = t
	}

	rtrn = NBT{Type: tt}

	// if the NBT type is TAG_End, there is no further data to read, not even a name, nor even a name-length telling us
	// there is no name; TAG_End items are just the type indicator itself, which is perfect for how they are used
	//
	if tt == TAG_End {
		return rtrn, nil
	}

	// NBT items other than TAG_End have the type indicator followed by a name-length and a name; however, the length
	// is permitted to be zero, in which case of course only the bytes encoding a zero-length are there, which simply
	// means the name is an empty string
	//
	// the use of the input parameter 't' as a sentinal value for TAG_List elements is used here, too, since TAG_List
	// elements are nameless, which is differnet from haveing a name of "" : there isn't even a name-length indicator
	//
	var strlen int16
	var name string
	if t == TAG_NULL {
		err = binary.Read(r, binary.BigEndian, &strlen)
		if err != nil {
			return rtrn, err
		}
		if strlen > 0 {
			data := make([]byte, strlen)
			_, err = io.ReadFull(r, data)
			if err != nil {
				return rtrn, err
			}
			name = string(data)
		} else {
			name = ""
		}
	} else {
		// since an emtpy string is a valid name, we use this as a sentinal value when writing NBT items back out
		// to indicated TAG_List elements, for which we must skip both the name and the name-length; yes, there is
		// potential collision with NBT items named "LISTELEM", but Minecraft does not currently do that anywhere
		name = "LISTELEM"
	}

	rtrn.Name = name

	if debug != "" {
		debug = debug + fmt.Sprintf("; type %s; name %s", tt, name)
		fmt.Printf("%s\n", debug)
	}

	var b byte
	switch tt {
	case TAG_Byte:
		b, err = r.ReadByte()
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = b

	case TAG_Short:
		var datashort int16
		err = binary.Read(r, binary.BigEndian, &datashort)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = datashort

	case TAG_Int:
		var dataint int32
		err = binary.Read(r, binary.BigEndian, &dataint)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = dataint

	case TAG_Long:
		var datalong int64
		err = binary.Read(r, binary.BigEndian, &datalong)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = datalong

	case TAG_Float:
		var datafloat float32
		err = binary.Read(r, binary.BigEndian, &datafloat)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = datafloat

	case TAG_Double:
		var datadouble float64
		err = binary.Read(r, binary.BigEndian, &datadouble)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = datadouble

	case TAG_String:
		var strlen int16
		err = binary.Read(r, binary.BigEndian, &strlen)
		if err != nil {
			return rtrn, err
		}
		rtrn.Size = uint32(strlen)

		data := make([]byte, strlen)
		_, err = io.ReadFull(r, data)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = string(data)

	case TAG_Byte_Array:
		var sizeint uint32
		err = binary.Read(r, binary.BigEndian, &sizeint)
		if err != nil {
			return rtrn, err
		}
		rtrn.Size = sizeint

		arraybyte := make([]byte, sizeint)
		err = binary.Read(r, binary.BigEndian, &arraybyte)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = arraybyte

	case TAG_Int_Array:
		var sizeint uint32
		err = binary.Read(r, binary.BigEndian, &sizeint)
		if err != nil {
			return rtrn, err
		}
		rtrn.Size = sizeint

		arrayint := make([]int32, sizeint)
		err = binary.Read(r, binary.BigEndian, &arrayint)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = arrayint

	case TAG_Long_Array:
		var sizeint uint32
		err = binary.Read(r, binary.BigEndian, &sizeint)
		if err != nil {
			return rtrn, err
		}
		rtrn.Size = sizeint

		arraylong := make([]int64, sizeint)
		err = binary.Read(r, binary.BigEndian, &arraylong)
		if err != nil {
			return rtrn, err
		}

		rtrn.Data = arraylong

	case TAG_List:
		// TAG_List NBT items include in their payload a byte indicating the NBT type of the elements of the
		// forthcoming List; this is one reason the List elements do not also bear the usual TAG_Type byte
		var id byte
		id, err = r.ReadByte()
		if err != nil {
			return rtrn, err
		}
		rtrn.List = NBTTAG(id)

		var sizeint uint32
		err = binary.Read(r, binary.BigEndian, &sizeint)
		if err != nil {
			return rtrn, err
		}
		rtrn.Size = sizeint

		// the Data of a TAG_List NBT item is an array of NBT items
		listnbt := make([]NBT, sizeint)

		// we use a recursive call to this function to read in the List elements; along with TAG_Compound, this
		// manifests the hierarchical nature of the NBT encoding scheme;  for these List elements, though, we send
		// in the TAG_Type of the List elements; see code comments at the top of this function for more detail why
		for indx := 0; indx < int(sizeint); indx++ {
			listnbt[indx], err = ReadNBTData(r, NBTTAG(id), debug)
			if err != nil {
				return rtrn, err
			}
		}

		// the Data of a TAG_List NBT item is an array of NBT items
		rtrn.Data = listnbt

	case TAG_Compound:
		// the Data of a TAG_Compound NBT item is a collection of fully-formed NBT items
		rtrn.Data = make([]NBT, 0)
		rtrn.Size = 0

		var nbt NBT
		for {
			// we use a recursive call to this function to read in the Compound elements; along with TAG_List,
			// this manifests the hierarchical nature of the NBT encoding scheme;  unlike TAG_List, each
			// TAG_Compound element is a fully-formed NBT item, so we call ReadNBTData() in the normal manner
			nbt, err = ReadNBTData(r, TAG_NULL, debug)
			if err != nil {
				return rtrn, err
			}

			// TAG_Compound has no other way to indicate the end of the collection, other than TAG_End; we do
			// not store this; instead, when will write out a TAG_End after the loop for wrting the TAG_Compound
			if nbt.Type == TAG_End {
				break
			}

			// we track and store the size of this TAG_Compound item, for potential future usefulness; this is
			// not written back out when we write the NBT data
			rtrn.Size++

			// the Data of a TAG_Compound NBT item is a collection of fully-formed NBT items
			tmparr := rtrn.Data.([]NBT)
			tmparr = append(tmparr, nbt)
			rtrn.Data = tmparr
		}

	default:
		return rtrn, fmt.Errorf("ReadNBTData : TAG type unkown: %d", tt)
	}

	return rtrn, err
}

func WriteNBTData(buf *bytes.Buffer, src *NBT) (err error) {
	// if we reach this point with an NBTTAG bearing our internal NULL-type TAG or nil data,
	// something went wrong somewhere, so we abend
	if src.Type == TAG_NULL {
		return fmt.Errorf("WriteNBTData : attempted to write a TAG with NULL type")
	}

	if src.Data == nil {
		return fmt.Errorf("WriteNBTData : attempted to write a TAG witn nil data")
	}

	// if the Name of this NBTTAG is "LISTELEM", then it is an element of a TAG_List, and we store only the payload; the
	// type of the list elements has already been stored at the start of the TAG_List, and each element is nameless, not
	// even having the 0-byte normally used to indicate a 0-length name
	//
	// otherwise, it is a named TAG, so before storing the payload, we store the TAG type, the length of the name and the
	// name itself; although the name might be zero-length
	if src.Name != "LISTELEM" {
		err = binary.Write(buf, binary.BigEndian, byte(src.Type))
		if err != nil {
			return err
		}

		// TAG_End never has a name, nor a name length, so we are done after just storing the type
		if src.Type == TAG_End {
			return nil
		}

		strlen := len(src.Name)
		err = binary.Write(buf, binary.BigEndian, int16(strlen))
		if err != nil {
			return err
		}

		if strlen > 0 {
			_, err = buf.WriteString(src.Name)
			if err != nil {
				return err
			}
		}
	}

	switch src.Type {
	case TAG_Byte:
		err = binary.Write(buf, binary.BigEndian, src.Data.(byte))
		if err != nil {
			return err
		}

	case TAG_Short:
		err = binary.Write(buf, binary.BigEndian, src.Data.(int16))
		if err != nil {
			return err
		}

	case TAG_Int:
		err = binary.Write(buf, binary.BigEndian, src.Data.(int32))
		if err != nil {
			return err
		}

	case TAG_Long:
		err = binary.Write(buf, binary.BigEndian, src.Data.(int64))
		if err != nil {
			return err
		}

	case TAG_Float:
		err = binary.Write(buf, binary.BigEndian, src.Data.(float32))
		if err != nil {
			return err
		}

	case TAG_Double:
		err = binary.Write(buf, binary.BigEndian, src.Data.(float64))
		if err != nil {
			return err
		}

	case TAG_String:
		strlen := len(src.Data.(string))
		err = binary.Write(buf, binary.BigEndian, int16(strlen))
		if err != nil {
			return err
		}

		if strlen > 0 {
			_, err = buf.WriteString(src.Data.(string))
			if err != nil {
				return err
			}
		}

	case TAG_Byte_Array:
		err = binary.Write(buf, binary.BigEndian, src.Size)
		if err != nil {
			return err
		}

		err = binary.Write(buf, binary.BigEndian, src.Data.([]byte))
		if err != nil {
			return err
		}

	case TAG_Int_Array:
		err = binary.Write(buf, binary.BigEndian, src.Size)
		if err != nil {
			return err
		}

		err = binary.Write(buf, binary.BigEndian, src.Data.([]int32))
		if err != nil {
			return err
		}

	case TAG_Long_Array:
		err = binary.Write(buf, binary.BigEndian, src.Size)
		if err != nil {
			return err
		}

		err = binary.Write(buf, binary.BigEndian, src.Data.([]int64))
		if err != nil {
			return err
		}

	case TAG_List:
		id := src.List
		err = binary.Write(buf, binary.BigEndian, byte(id))
		if err != nil {
			return err
		}

		err = binary.Write(buf, binary.BigEndian, src.Size)
		if err != nil {
			return err
		}

		arrlen := len(src.Data.([]NBT))
		for indx := 0; indx < int(arrlen); indx++ {
			elem := src.Data.([]NBT)[indx]
			err = WriteNBTData(buf, &elem)
			if err != nil {
				return err
			}
		}

	case TAG_Compound:
		for _, elem := range src.Data.([]NBT) {
			err = WriteNBTData(buf, &elem)
			if err != nil {
				return err
			}
		}
		// we used the TAG_End at the end of a collection of TAG_Compound elements to break out of the reading loop;
		// so, we have not stored it; so, we write out a TAG_End NBT item after writing out all the Compound elements
		err = binary.Write(buf, binary.BigEndian, byte(TAG_End))
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("WriteNBTData : TAG type unkown: %d", src.Type)
	}

	return nil
}
