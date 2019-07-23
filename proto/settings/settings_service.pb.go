// Code generated by protoc-gen-go. DO NOT EDIT.
// source: settings/settings_service.proto

package settings

import (
	context "context"
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
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

func init() { proto.RegisterFile("settings/settings_service.proto", fileDescriptor_f19be0f58c8239c5) }

var fileDescriptor_f19be0f58c8239c5 = []byte{
	// 101 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x92, 0x2f, 0x4e, 0x2d, 0x29,
	0xc9, 0xcc, 0x4b, 0x2f, 0xd6, 0x87, 0x31, 0xe2, 0x8b, 0x53, 0x8b, 0xca, 0x32, 0x93, 0x53, 0xf5,
	0x0a, 0x8a, 0xf2, 0x4b, 0xf2, 0x85, 0x38, 0x60, 0xe2, 0x52, 0xe2, 0x18, 0x4a, 0x21, 0x4a, 0x8c,
	0x3c, 0xb9, 0xf8, 0x83, 0xa1, 0x22, 0xc1, 0x10, 0xbd, 0x42, 0x66, 0x5c, 0xcc, 0xee, 0xa9, 0x25,
	0x42, 0xa2, 0x7a, 0x70, 0xa5, 0x30, 0x15, 0x41, 0xa9, 0x85, 0x52, 0x62, 0xd8, 0x84, 0x8b, 0x0b,
	0x94, 0x18, 0x92, 0xd8, 0xc0, 0x26, 0x1a, 0x03, 0x02, 0x00, 0x00, 0xff, 0xff, 0x9f, 0xe2, 0x46,
	0x84, 0x97, 0x00, 0x00, 0x00,
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// SettingsServiceClient is the client API for SettingsService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type SettingsServiceClient interface {
	Get(ctx context.Context, in *SettingsReq, opts ...grpc.CallOption) (*SettingsResp, error)
}

type settingsServiceClient struct {
	cc *grpc.ClientConn
}

func NewSettingsServiceClient(cc *grpc.ClientConn) SettingsServiceClient {
	return &settingsServiceClient{cc}
}

func (c *settingsServiceClient) Get(ctx context.Context, in *SettingsReq, opts ...grpc.CallOption) (*SettingsResp, error) {
	out := new(SettingsResp)
	err := c.cc.Invoke(ctx, "/settings.SettingsService/Get", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SettingsServiceServer is the server API for SettingsService service.
type SettingsServiceServer interface {
	Get(context.Context, *SettingsReq) (*SettingsResp, error)
}

// UnimplementedSettingsServiceServer can be embedded to have forward compatible implementations.
type UnimplementedSettingsServiceServer struct {
}

func (*UnimplementedSettingsServiceServer) Get(ctx context.Context, req *SettingsReq) (*SettingsResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Get not implemented")
}

func RegisterSettingsServiceServer(s *grpc.Server, srv SettingsServiceServer) {
	s.RegisterService(&_SettingsService_serviceDesc, srv)
}

func _SettingsService_Get_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SettingsReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SettingsServiceServer).Get(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/settings.SettingsService/Get",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SettingsServiceServer).Get(ctx, req.(*SettingsReq))
	}
	return interceptor(ctx, in, info, handler)
}

var _SettingsService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "settings.SettingsService",
	HandlerType: (*SettingsServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Get",
			Handler:    _SettingsService_Get_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "settings/settings_service.proto",
}
