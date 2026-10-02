#!/usr/bin/env python3
"""Self-contained authoritative fixture and raw clients for isolated kernel tests."""
import ipaddress
import json
import secrets
import socket
import socketserver
import struct
import sys
import threading
import time


def name_end(data, pos=12):
    while data[pos]:
        if data[pos] & 0xc0 == 0xc0:
            return pos+2
        pos += data[pos]+1
    return pos+1


def make_answer(data):
    end=name_end(data)+4
    ident,flags=struct.unpack_from('!HH',data)
    qtype=struct.unpack_from('!H',data,end-4)[0]
    payload=None
    if qtype==1:payload=ipaddress.ip_address('198.51.100.7').packed
    if qtype==28:payload=ipaddress.ip_address('2001:db8::7').packed
    response=struct.pack('!HHHHHH',ident,0x8180|(flags&0x10),1,int(payload is not None),0,0)+data[12:end]
    if payload is not None:response+=b'\xc0\x0c'+struct.pack('!HHIH',qtype,1,30,len(payload))+payload
    return response


def recv_exact(conn,n):
    data=b''
    while len(data)<n:
        chunk=conn.recv(n-len(data))
        if not chunk:raise EOFError('short DNS TCP frame')
        data+=chunk
    return data


class UDP(socketserver.ThreadingUDPServer):
    address_family=socket.AF_INET6
    allow_reuse_address=True
    daemon_threads=True
    def server_bind(self):
        self.socket.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,0)
        super().server_bind()


class TCP(socketserver.ThreadingTCPServer):
    address_family=socket.AF_INET6
    allow_reuse_address=True
    daemon_threads=True
    def server_bind(self):
        self.socket.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,0)
        super().server_bind()


class UDPHandler(socketserver.BaseRequestHandler):
    def handle(self):
        data,sock=self.request
        sock.sendto(make_answer(data),self.client_address)


class TCPHandler(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(5)
        while True:
            try:
                n=struct.unpack('!H',recv_exact(self.request,2))[0]
                response=make_answer(recv_exact(self.request,n))
                self.request.sendall(struct.pack('!H',len(response))+response)
            except (EOFError,OSError):return


def query(host,qtype,name,transport='udp',source_port=0):
    ident=secrets.randbelow(65536)
    question=b''.join(bytes([len(part)])+part.encode() for part in name.strip('.').split('.'))+b'\x00'+struct.pack('!HH',qtype,1)
    data=struct.pack('!HHHHHH',ident,0x100,1,0,0,0)+question
    fam=socket.AF_INET6 if ':' in host else socket.AF_INET
    scope_id=0
    connect_host=host
    if fam==socket.AF_INET6 and '%' in host:
        connect_host,zone=host.rsplit('%',1)
        scope_id=socket.if_nametoindex(zone)
    sock=socket.socket(fam,socket.SOCK_STREAM if transport=='tcp' else socket.SOCK_DGRAM)
    sock.settimeout(4)
    if source_port:
        sock.bind(('::',source_port,0,0) if fam==socket.AF_INET6 else ('0.0.0.0',source_port))
    target=(connect_host,53,0,scope_id) if fam==socket.AF_INET6 else (connect_host,53)
    started=time.monotonic()
    try:
        sock.connect(target)
        if transport=='tcp':
            sock.sendall(struct.pack('!H',len(data))+data)
            response=recv_exact(sock,struct.unpack('!H',recv_exact(sock,2))[0])
        else:
            sock.send(data);response=sock.recv(65535)
    finally:sock.close()
    elapsed=time.monotonic()-started
    rid,flags,qd,an,ns,ar=struct.unpack_from('!HHHHHH',response)
    assert rid==ident and qd==1,(rid,ident,qd)
    pos=name_end(response)+4
    addresses=[]
    for _ in range(an):
        pos=name_end(response,pos)
        typ,cl,ttl,n=struct.unpack_from('!HHIH',response,pos);pos+=10
        if typ in (1,28):addresses.append(str(ipaddress.ip_address(response[pos:pos+n])))
        pos+=n
    return {'rcode':flags&15,'addresses':addresses,'elapsed':elapsed,'transport':transport,'family':4 if fam==socket.AF_INET else 6}


def main():
    if sys.argv[1]=='serve':
        udp=UDP(('::',53),UDPHandler);tcp=TCP(('::',53),TCPHandler)
        threading.Thread(target=udp.serve_forever,daemon=True).start();tcp.serve_forever()
    elif sys.argv[1]=='query':
        print(json.dumps(query(sys.argv[2],int(sys.argv[3]),sys.argv[4],sys.argv[5],int(sys.argv[6]) if len(sys.argv)>6 else 0)))
    elif sys.argv[1]=='echo':
        sock=socket.socket(socket.AF_INET6,socket.SOCK_DGRAM);sock.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,0);sock.bind(('::',9999))
        while True:
            data,addr=sock.recvfrom(2048);sock.sendto(data,addr)
    elif sys.argv[1]=='traffic':
        host=sys.argv[2];sock=socket.socket(socket.AF_INET6 if ':' in host else socket.AF_INET,socket.SOCK_DGRAM);sock.settimeout(3)
        for _ in range(20):sock.sendto(b'x'*1000,(host,9999));assert len(sock.recv(2048))==1000
        print('echo traffic complete',host)

if __name__=='__main__':main()
