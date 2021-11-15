package main

import (
	"bytes"
	"compress/zlib"
	"debug/elf"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"test.com/a/breakpad"
	"unicode/utf16"
	"unicode/utf8"
)

var MpParse = make(map[string]*NodeData)

func UTF16BytesToString(b []byte, o binary.ByteOrder) string {
	utf := make([]uint16, (len(b)+(2-1))/2)
	for i := 0; i+(2-1) < len(b); i += 2 {
		utf[i/2] = o.Uint16(b[i:])
	}
	if len(b)/2 < len(utf) {
		utf[len(utf)-1] = utf8.RuneError
	}
	return string(utf16.Decode(utf))
}

var mapId map[int64]bool = map[int64]bool{}

type QtResNode struct {
	Offset uint32
	Flags  uint16
	Count  uint32
	NodeId uint32
}
type QtResName struct {
	Length uint16
	Hash   uint32
	Name   []byte
}
type QtResFile struct {
	Offset   uint32
	Flags    uint16
	Country  uint16
	Language uint16
	Offset1  uint32
}
type QtResFileData struct {
	Length uint32
	Data   []byte
}

func (q *QtResName) NameString() string {
	return UTF16BytesToString(q.Name, binary.BigEndian)
}
func readEntryFile(qt_resource_struct_data []byte, qt_resource_data []byte, entry_offset uint32) (*QtResFile, *QtResFileData) {
	rd := bytes.NewReader(qt_resource_struct_data[entry_offset:])
	var file QtResFile
	binary.Read(rd, binary.BigEndian, &file.Offset)
	binary.Read(rd, binary.BigEndian, &file.Flags)
	binary.Read(rd, binary.BigEndian, &file.Country)
	binary.Read(rd, binary.BigEndian, &file.Language)
	binary.Read(rd, binary.BigEndian, &file.Offset1)

	rd = bytes.NewReader(qt_resource_data[file.Offset1:])
	var data QtResFileData
	binary.Read(rd, binary.BigEndian, &data.Length)
	data.Data = make([]byte, data.Length)
	binary.Read(rd, binary.BigEndian, &data.Data)
	return &file, &data
}
func readEntryName(qt_resource_name_data []byte, name_offset uint32) *QtResName {
	var name QtResName
	rbytes := qt_resource_name_data[name_offset:]
	rd := bytes.NewReader(rbytes)
	binary.Read(rd, binary.BigEndian, &name.Length)
	binary.Read(rd, binary.BigEndian, &name.Hash)
	name.Name = make([]byte, name.Length*2)
	binary.Read(rd, binary.BigEndian, &name.Name)
	return &name
}
func readEntryNode(qt_resource_struct_data []byte, entry_offset uint32) *QtResNode {
	rd := bytes.NewReader(qt_resource_struct_data[entry_offset:])
	var node QtResNode
	binary.Read(rd, binary.BigEndian, &node.Offset)
	binary.Read(rd, binary.BigEndian, &node.Flags)
	binary.Read(rd, binary.BigEndian, &node.Count)
	binary.Read(rd, binary.BigEndian, &node.NodeId)
	return &node
}
func readEntry(qt_resource_struct_data, qt_resource_name_data, qt_resource_data_data []byte, entry_offset int64, mnode *MNode) {
	if mapId[entry_offset] == true {
		return
	}
	mapId[entry_offset] = true
	node := readEntryNode(qt_resource_struct_data, uint32(entry_offset))
	name := readEntryName(qt_resource_name_data, node.Offset)
	if node.Flags == 2 {
		//fmt.Printf("%s 目录 ID:%d,总数:%d\n", name.NameString(), node.NodeId, node.Count)
		newMNode := &MNode{
			Tag:  name.NameString(),
			Val:  &NodeData{Node: node},
			Path: path.Join(mnode.Path, name.NameString()),
		}
		mnode.Children = append(mnode.Children, newMNode)
		for id := node.NodeId; id < node.NodeId+node.Count; id++ {
			readEntry(qt_resource_struct_data, qt_resource_name_data, qt_resource_data_data, int64(id*14), newMNode)
		}
	} else {
		qtFile, qtData := readEntryFile(qt_resource_struct_data, qt_resource_data_data, uint32(entry_offset))
		if qtFile.Flags == 1 {
			rd := bytes.NewReader(qtData.Data)
			l := int32(0)
			binary.Read(rd, binary.BigEndian, &l)
			var out bytes.Buffer
			r, _ := zlib.NewReader(rd)
			io.Copy(&out, r)
			qtData.Data = out.Bytes()
		}
		newMNode := &MNode{
			Tag:  name.NameString(),
			Val:  &NodeData{Node: node, FileData: qtData, FileOffset: uint64(qtFile.Offset1 + 4), FileNode: qtFile},
			Path: path.Join(mnode.Path, name.NameString()),
		}
		mnode.Children = append(mnode.Children, newMNode)

		//fmt.Printf("%s 文件 ID:%d 总数:%d\n", name.NameString(), node.NodeId, node.Count)
	}
}
func GetResource(qt_resource_struct_data, qt_resource_name_data, qt_resource_data_data []byte) (map[string]*NodeData, error) {
	size := len(qt_resource_struct_data)
	node := &MNode{Tag: "root", Path: "./"}
	i := int64(0)
	for entry_offset := int64(0); entry_offset < int64(size); entry_offset = i * 14 {
		if entry_offset > int64(size) {
			break
		}
		readEntry(qt_resource_struct_data, qt_resource_name_data, qt_resource_data_data, entry_offset, node)
		i++
	}
	mp := make(map[string]*NodeData)
	TreeToMap(node, mp)
	return mp, nil
}
func ParseBin(fname string) error {
	fbytes, err := ioutil.ReadFile(fname)
	if err != nil {
		return err
	}
	_ = fbytes
	f, err := os.Open("lm")
	if err != nil {
		return err
	}
	defer f.Close()
	elff, err := elf.NewFile(f)
	if err != nil {
		return err
	}
	defer elff.Close()
	symbols, err := elff.Symbols()
	if err != nil {
		return err
	}
	section := elff.Section(".rodata")
	var qt_resource_struct elf.Symbol
	var qt_resource_struct_data []byte
	var qt_resource_name elf.Symbol
	var qt_resource_name_data []byte
	var qt_resource_data elf.Symbol
	var qt_resource_data_data []byte
	for _, sym := range symbols {
		if strings.HasSuffix(sym.Name, "qt_resource_struct") {
			qt_resource_struct = sym
		} else if strings.HasSuffix(sym.Name, "qt_resource_name") {
			qt_resource_name = sym
		} else if strings.HasSuffix(sym.Name, "qt_resource_data") {
			qt_resource_data = sym
		}
	}
	offset := (qt_resource_struct.Value - section.Addr) + section.Offset
	qt_resource_struct_data = fbytes[offset : offset+qt_resource_struct.Size]
	offset = (qt_resource_name.Value - section.Addr) + section.Offset
	qt_resource_name_data = fbytes[offset : offset+qt_resource_name.Size]
	offset = (qt_resource_data.Value - section.Addr) + section.Offset
	qt_resource_data_data = fbytes[offset : offset+qt_resource_data.Size]
	MpParse, err = GetResource(qt_resource_struct_data, qt_resource_name_data, qt_resource_data_data)
	if err != nil {
		return err
	}
	for k, v := range MpParse {
		_ = k
		v.FileOffset = v.FileOffset + offset
	}
	return nil
}
func init() {
	breakpad.Init("崩溃日志.txt")
}
func main() {
	err := ParseBin("lm")
	if err != nil {
		panic(err)
	}
	if os.Getenv("D") == "1" {
		for k, v := range MpParse {
			fmt.Printf("文件:%s 大小:%d 偏移:%08X 结束:%08X  是否压缩:%b\n", k, len(v.FileData.Data), v.FileOffset, v.FileOffset+uint64(v.FileData.Length), v.FileNode.Flags)
		}
	}

	in := flag.Bool("in", false, "是否使用输入模式")
	flag.Parse()
	if !flag.Parsed() {
		flag.PrintDefaults()
		return
	}
	if *in == false {
		os.RemoveAll("./输出")
		os.MkdirAll("./输出", fs.ModePerm)
		for k, v := range MpParse {
			fullPath := path.Join("./输出", k)
			dr := filepath.Dir(fullPath)
			os.MkdirAll(dr, fs.ModePerm)
			os.WriteFile(fullPath, v.FileData.Data, fs.ModePerm)
		}
		fmt.Println("输出模式,执行完成")
	} else {
		err := func() error {
			os.MkdirAll("修改", fs.ModePerm)
			os.RemoveAll("./修改/lm")
			oldLmByte, err := ioutil.ReadFile("lm")
			if err != nil {
				return err
			}
			f, err := os.Create("./修改/lm")
			if err != nil {
				return err
			}
			f.Write(oldLmByte)
			defer f.Close()
			err = filepath.Walk("./输入", func(path string, info fs.FileInfo, err error) error {
				if info.IsDir() == true {
					return nil
				}
				newPath := strings.ReplaceAll(path, "输入\\", "")
				newPath = strings.ReplaceAll(newPath, "\\", "/")
				replaceD := MpParse[newPath]
				if replaceD == nil {
					return errors.New(fmt.Sprintf("不存在这个文件:%s", newPath))
				}
				newData, err := ioutil.ReadFile(path)
				if err != nil {
					return err
				}
				if replaceD.FileNode.Flags != 1 {
					if len(newData) > len(replaceD.FileData.Data) {
						return errors.New(fmt.Sprintf("资源大小大于源文件无法替换:%s", newPath))
					}
					f.Seek(int64(replaceD.FileOffset), io.SeekStart)
					f.Write(make([]byte, replaceD.FileData.Length))
					f.Seek(int64(replaceD.FileOffset), io.SeekStart)
					f.Write(newData)
				} else {
					newZlibData := DoZlibCompress(replaceD.FileData.Data)
					if len(newZlibData) > len(replaceD.FileData.Data) {
						return errors.New(fmt.Sprintf("资源压缩后，大小大于源文件无法替换:%s", newPath))
					}
					f.Seek(int64(replaceD.FileOffset), io.SeekStart)
					f.Write(make([]byte, replaceD.FileData.Length))

					f.Seek(int64(replaceD.FileOffset), io.SeekStart)
					binary.Write(f, binary.BigEndian, uint32(len(newZlibData)))
					f.Write(newZlibData)
				}
				return nil
			})
			if err != nil {
				return err
			}
			return nil
		}()
		if err != nil {
			panic(err.Error())
		}
		fmt.Println("输入模式,执行完成")
	}
}

func DoZlibCompress(src []byte) []byte {
	var in bytes.Buffer
	w, err := zlib.NewWriterLevel(&in, zlib.BestCompression)
	if err != nil {
		panic(err)
	}
	w.Write(src)
	w.Close()
	return in.Bytes()
}
