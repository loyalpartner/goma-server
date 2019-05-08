// Code generated by protoc-gen-go. DO NOT EDIT.
// source: cache/cache.proto

package cache

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

type KV struct {
	Key                  string   `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Value                []byte   `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *KV) Reset()         { *m = KV{} }
func (m *KV) String() string { return proto.CompactTextString(m) }
func (*KV) ProtoMessage()    {}
func (*KV) Descriptor() ([]byte, []int) {
	return fileDescriptor_dd209d76f5b70ea3, []int{0}
}

func (m *KV) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_KV.Unmarshal(m, b)
}
func (m *KV) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_KV.Marshal(b, m, deterministic)
}
func (m *KV) XXX_Merge(src proto.Message) {
	xxx_messageInfo_KV.Merge(m, src)
}
func (m *KV) XXX_Size() int {
	return xxx_messageInfo_KV.Size(m)
}
func (m *KV) XXX_DiscardUnknown() {
	xxx_messageInfo_KV.DiscardUnknown(m)
}

var xxx_messageInfo_KV proto.InternalMessageInfo

func (m *KV) GetKey() string {
	if m != nil {
		return m.Key
	}
	return ""
}

func (m *KV) GetValue() []byte {
	if m != nil {
		return m.Value
	}
	return nil
}

type GetReq struct {
	Key                  string   `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Fast                 bool     `protobuf:"varint,2,opt,name=fast,proto3" json:"fast,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *GetReq) Reset()         { *m = GetReq{} }
func (m *GetReq) String() string { return proto.CompactTextString(m) }
func (*GetReq) ProtoMessage()    {}
func (*GetReq) Descriptor() ([]byte, []int) {
	return fileDescriptor_dd209d76f5b70ea3, []int{1}
}

func (m *GetReq) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_GetReq.Unmarshal(m, b)
}
func (m *GetReq) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_GetReq.Marshal(b, m, deterministic)
}
func (m *GetReq) XXX_Merge(src proto.Message) {
	xxx_messageInfo_GetReq.Merge(m, src)
}
func (m *GetReq) XXX_Size() int {
	return xxx_messageInfo_GetReq.Size(m)
}
func (m *GetReq) XXX_DiscardUnknown() {
	xxx_messageInfo_GetReq.DiscardUnknown(m)
}

var xxx_messageInfo_GetReq proto.InternalMessageInfo

func (m *GetReq) GetKey() string {
	if m != nil {
		return m.Key
	}
	return ""
}

func (m *GetReq) GetFast() bool {
	if m != nil {
		return m.Fast
	}
	return false
}

type GetResp struct {
	Kv                   *KV      `protobuf:"bytes,1,opt,name=kv,proto3" json:"kv,omitempty"`
	InMemory             bool     `protobuf:"varint,2,opt,name=in_memory,json=inMemory,proto3" json:"in_memory,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *GetResp) Reset()         { *m = GetResp{} }
func (m *GetResp) String() string { return proto.CompactTextString(m) }
func (*GetResp) ProtoMessage()    {}
func (*GetResp) Descriptor() ([]byte, []int) {
	return fileDescriptor_dd209d76f5b70ea3, []int{2}
}

func (m *GetResp) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_GetResp.Unmarshal(m, b)
}
func (m *GetResp) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_GetResp.Marshal(b, m, deterministic)
}
func (m *GetResp) XXX_Merge(src proto.Message) {
	xxx_messageInfo_GetResp.Merge(m, src)
}
func (m *GetResp) XXX_Size() int {
	return xxx_messageInfo_GetResp.Size(m)
}
func (m *GetResp) XXX_DiscardUnknown() {
	xxx_messageInfo_GetResp.DiscardUnknown(m)
}

var xxx_messageInfo_GetResp proto.InternalMessageInfo

func (m *GetResp) GetKv() *KV {
	if m != nil {
		return m.Kv
	}
	return nil
}

func (m *GetResp) GetInMemory() bool {
	if m != nil {
		return m.InMemory
	}
	return false
}

type PutReq struct {
	Kv                   *KV      `protobuf:"bytes,1,opt,name=kv,proto3" json:"kv,omitempty"`
	WriteBack            bool     `protobuf:"varint,2,opt,name=write_back,json=writeBack,proto3" json:"write_back,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PutReq) Reset()         { *m = PutReq{} }
func (m *PutReq) String() string { return proto.CompactTextString(m) }
func (*PutReq) ProtoMessage()    {}
func (*PutReq) Descriptor() ([]byte, []int) {
	return fileDescriptor_dd209d76f5b70ea3, []int{3}
}

func (m *PutReq) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PutReq.Unmarshal(m, b)
}
func (m *PutReq) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PutReq.Marshal(b, m, deterministic)
}
func (m *PutReq) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PutReq.Merge(m, src)
}
func (m *PutReq) XXX_Size() int {
	return xxx_messageInfo_PutReq.Size(m)
}
func (m *PutReq) XXX_DiscardUnknown() {
	xxx_messageInfo_PutReq.DiscardUnknown(m)
}

var xxx_messageInfo_PutReq proto.InternalMessageInfo

func (m *PutReq) GetKv() *KV {
	if m != nil {
		return m.Kv
	}
	return nil
}

func (m *PutReq) GetWriteBack() bool {
	if m != nil {
		return m.WriteBack
	}
	return false
}

type PutResp struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PutResp) Reset()         { *m = PutResp{} }
func (m *PutResp) String() string { return proto.CompactTextString(m) }
func (*PutResp) ProtoMessage()    {}
func (*PutResp) Descriptor() ([]byte, []int) {
	return fileDescriptor_dd209d76f5b70ea3, []int{4}
}

func (m *PutResp) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PutResp.Unmarshal(m, b)
}
func (m *PutResp) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PutResp.Marshal(b, m, deterministic)
}
func (m *PutResp) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PutResp.Merge(m, src)
}
func (m *PutResp) XXX_Size() int {
	return xxx_messageInfo_PutResp.Size(m)
}
func (m *PutResp) XXX_DiscardUnknown() {
	xxx_messageInfo_PutResp.DiscardUnknown(m)
}

var xxx_messageInfo_PutResp proto.InternalMessageInfo

func init() {
	proto.RegisterType((*KV)(nil), "cache.KV")
	proto.RegisterType((*GetReq)(nil), "cache.GetReq")
	proto.RegisterType((*GetResp)(nil), "cache.GetResp")
	proto.RegisterType((*PutReq)(nil), "cache.PutReq")
	proto.RegisterType((*PutResp)(nil), "cache.PutResp")
}

func init() { proto.RegisterFile("cache/cache.proto", fileDescriptor_dd209d76f5b70ea3) }

var fileDescriptor_dd209d76f5b70ea3 = []byte{
	// 203 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x12, 0x4c, 0x4e, 0x4c, 0xce,
	0x48, 0xd5, 0x07, 0x93, 0x7a, 0x05, 0x45, 0xf9, 0x25, 0xf9, 0x42, 0xac, 0x60, 0x8e, 0x92, 0x0e,
	0x17, 0x93, 0x77, 0x98, 0x90, 0x00, 0x17, 0x73, 0x76, 0x6a, 0xa5, 0x04, 0xa3, 0x02, 0xa3, 0x06,
	0x67, 0x10, 0x88, 0x29, 0x24, 0xc2, 0xc5, 0x5a, 0x96, 0x98, 0x53, 0x9a, 0x2a, 0xc1, 0xa4, 0xc0,
	0xa8, 0xc1, 0x13, 0x04, 0xe1, 0x28, 0xe9, 0x71, 0xb1, 0xb9, 0xa7, 0x96, 0x04, 0xa5, 0x16, 0x62,
	0xd1, 0x21, 0xc4, 0xc5, 0x92, 0x96, 0x58, 0x5c, 0x02, 0xd6, 0xc0, 0x11, 0x04, 0x66, 0x2b, 0x39,
	0x72, 0xb1, 0x83, 0xd5, 0x17, 0x17, 0x08, 0x49, 0x72, 0x31, 0x65, 0x97, 0x81, 0xd5, 0x73, 0x1b,
	0x71, 0xea, 0x41, 0x5c, 0xe2, 0x1d, 0x16, 0xc4, 0x94, 0x5d, 0x26, 0x24, 0xcd, 0xc5, 0x99, 0x99,
	0x17, 0x9f, 0x9b, 0x9a, 0x9b, 0x5f, 0x54, 0x09, 0xd5, 0xce, 0x91, 0x99, 0xe7, 0x0b, 0xe6, 0x2b,
	0x39, 0x71, 0xb1, 0x05, 0x94, 0x82, 0xad, 0xc4, 0x63, 0x82, 0x2c, 0x17, 0x57, 0x79, 0x51, 0x66,
	0x49, 0x6a, 0x7c, 0x52, 0x62, 0x72, 0x36, 0xd4, 0x08, 0x4e, 0xb0, 0x88, 0x53, 0x62, 0x72, 0xb6,
	0x12, 0x27, 0x17, 0x3b, 0xd8, 0x8c, 0xe2, 0x82, 0x24, 0x36, 0xb0, 0xef, 0x8d, 0x01, 0x01, 0x00,
	0x00, 0xff, 0xff, 0xbf, 0x7e, 0xd5, 0xe5, 0x12, 0x01, 0x00, 0x00,
}