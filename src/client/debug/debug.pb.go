// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: client/debug/debug.proto

/*
Package debug is a generated protocol buffer package.

It is generated from these files:
	client/debug/debug.proto

It has these top-level messages:
	DumpRequest
*/
package debug

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import google_protobuf "github.com/gogo/protobuf/types"

import context "golang.org/x/net/context"
import grpc "google.golang.org/grpc"

import io "io"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type DumpRequest struct {
}

func (m *DumpRequest) Reset()                    { *m = DumpRequest{} }
func (m *DumpRequest) String() string            { return proto.CompactTextString(m) }
func (*DumpRequest) ProtoMessage()               {}
func (*DumpRequest) Descriptor() ([]byte, []int) { return fileDescriptorDebug, []int{0} }

func init() {
	proto.RegisterType((*DumpRequest)(nil), "debug.DumpRequest")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for Debug service

type DebugClient interface {
	Dump(ctx context.Context, in *DumpRequest, opts ...grpc.CallOption) (Debug_DumpClient, error)
}

type debugClient struct {
	cc *grpc.ClientConn
}

func NewDebugClient(cc *grpc.ClientConn) DebugClient {
	return &debugClient{cc}
}

func (c *debugClient) Dump(ctx context.Context, in *DumpRequest, opts ...grpc.CallOption) (Debug_DumpClient, error) {
	stream, err := grpc.NewClientStream(ctx, &_Debug_serviceDesc.Streams[0], c.cc, "/debug.Debug/Dump", opts...)
	if err != nil {
		return nil, err
	}
	x := &debugDumpClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Debug_DumpClient interface {
	Recv() (*google_protobuf.BytesValue, error)
	grpc.ClientStream
}

type debugDumpClient struct {
	grpc.ClientStream
}

func (x *debugDumpClient) Recv() (*google_protobuf.BytesValue, error) {
	m := new(google_protobuf.BytesValue)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Server API for Debug service

type DebugServer interface {
	Dump(*DumpRequest, Debug_DumpServer) error
}

func RegisterDebugServer(s *grpc.Server, srv DebugServer) {
	s.RegisterService(&_Debug_serviceDesc, srv)
}

func _Debug_Dump_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(DumpRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(DebugServer).Dump(m, &debugDumpServer{stream})
}

type Debug_DumpServer interface {
	Send(*google_protobuf.BytesValue) error
	grpc.ServerStream
}

type debugDumpServer struct {
	grpc.ServerStream
}

func (x *debugDumpServer) Send(m *google_protobuf.BytesValue) error {
	return x.ServerStream.SendMsg(m)
}

var _Debug_serviceDesc = grpc.ServiceDesc{
	ServiceName: "debug.Debug",
	HandlerType: (*DebugServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Dump",
			Handler:       _Debug_Dump_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "client/debug/debug.proto",
}

func (m *DumpRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *DumpRequest) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	return i, nil
}

func encodeVarintDebug(dAtA []byte, offset int, v uint64) int {
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return offset + 1
}
func (m *DumpRequest) Size() (n int) {
	var l int
	_ = l
	return n
}

func sovDebug(x uint64) (n int) {
	for {
		n++
		x >>= 7
		if x == 0 {
			break
		}
	}
	return n
}
func sozDebug(x uint64) (n int) {
	return sovDebug(uint64((x << 1) ^ uint64((int64(x) >> 63))))
}
func (m *DumpRequest) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowDebug
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: DumpRequest: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: DumpRequest: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		default:
			iNdEx = preIndex
			skippy, err := skipDebug(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthDebug
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func skipDebug(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowDebug
			}
			if iNdEx >= l {
				return 0, io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowDebug
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				iNdEx++
				if dAtA[iNdEx-1] < 0x80 {
					break
				}
			}
			return iNdEx, nil
		case 1:
			iNdEx += 8
			return iNdEx, nil
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowDebug
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			iNdEx += length
			if length < 0 {
				return 0, ErrInvalidLengthDebug
			}
			return iNdEx, nil
		case 3:
			for {
				var innerWire uint64
				var start int = iNdEx
				for shift := uint(0); ; shift += 7 {
					if shift >= 64 {
						return 0, ErrIntOverflowDebug
					}
					if iNdEx >= l {
						return 0, io.ErrUnexpectedEOF
					}
					b := dAtA[iNdEx]
					iNdEx++
					innerWire |= (uint64(b) & 0x7F) << shift
					if b < 0x80 {
						break
					}
				}
				innerWireType := int(innerWire & 0x7)
				if innerWireType == 4 {
					break
				}
				next, err := skipDebug(dAtA[start:])
				if err != nil {
					return 0, err
				}
				iNdEx = start + next
			}
			return iNdEx, nil
		case 4:
			return iNdEx, nil
		case 5:
			iNdEx += 4
			return iNdEx, nil
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
	}
	panic("unreachable")
}

var (
	ErrInvalidLengthDebug = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowDebug   = fmt.Errorf("proto: integer overflow")
)

func init() { proto.RegisterFile("client/debug/debug.proto", fileDescriptorDebug) }

var fileDescriptorDebug = []byte{
	// 157 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x92, 0x48, 0xce, 0xc9, 0x4c,
	0xcd, 0x2b, 0xd1, 0x4f, 0x49, 0x4d, 0x2a, 0x4d, 0x87, 0x90, 0x7a, 0x05, 0x45, 0xf9, 0x25, 0xf9,
	0x42, 0xac, 0x60, 0x8e, 0x94, 0x5c, 0x7a, 0x7e, 0x7e, 0x7a, 0x4e, 0xaa, 0x3e, 0x58, 0x30, 0xa9,
	0x34, 0x4d, 0xbf, 0xbc, 0x28, 0xb1, 0xa0, 0x20, 0xb5, 0xa8, 0x18, 0xa2, 0x4c, 0x89, 0x97, 0x8b,
	0xdb, 0xa5, 0x34, 0xb7, 0x20, 0x28, 0xb5, 0xb0, 0x34, 0xb5, 0xb8, 0xc4, 0xc8, 0x85, 0x8b, 0xd5,
	0x05, 0xa4, 0x4f, 0xc8, 0x9a, 0x8b, 0x05, 0x24, 0x2e, 0x24, 0xa4, 0x07, 0x31, 0x14, 0x49, 0x91,
	0x94, 0xb4, 0x1e, 0xc4, 0x50, 0x3d, 0x98, 0xa1, 0x7a, 0x4e, 0x95, 0x25, 0xa9, 0xc5, 0x61, 0x89,
	0x39, 0xa5, 0xa9, 0x4a, 0x0c, 0x06, 0x8c, 0x4e, 0x02, 0x27, 0x1e, 0xc9, 0x31, 0x5e, 0x78, 0x24,
	0xc7, 0xf8, 0xe0, 0x91, 0x1c, 0xe3, 0x8c, 0xc7, 0x72, 0x0c, 0x49, 0x6c, 0x60, 0xa5, 0xc6, 0x80,
	0x00, 0x00, 0x00, 0xff, 0xff, 0x31, 0xc8, 0xc1, 0x2c, 0xb0, 0x00, 0x00, 0x00,
}
