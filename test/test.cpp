#include "libhole.h"

#define MAX_NIC 3

using namespace std;

int main(int argc, char* argv[])
{
	if (argc < 4)
	{
		cout << "arg : MyPort DstIp DstPort\n";
		return -1;
	}

	int srcPort = atoi(argv[1]);
	std::string dstIp = argv[2];
	int dstPort = atoi(argv[3]);

	initNetwork();

	StunAddress4 stunServerAddr;
	std::string stunServerHost = "stun.schlund.de";
	bool ret = stunParseServerName(stunServerHost.c_str(), stunServerAddr);
	if (!ret)
	{
		cout << "stunParseServerName Fail\n";
		return -1;
	}

	NatType stype = stunNatType( stunServerAddr, false, 0, 0, 
		srcPort, 0);

	cout << "stunNatType " << stype << std::endl;
	if (stype == StunTypeBlocked)
	{
		return -1;
	}

	StunAddress4 mappedAddr;
	int fd = stunOpenSocket(stunServerAddr, &mappedAddr,
		srcPort, 0,
		false);

  	cout << "stunOpenSocket OK " << mappedAddr << std::endl;

	{
		char c;
		std::cin>>c;
	}

	char data[100] = {0};
	int i = 0;
	while (1)
	{
		in_addr tmp;
		*(int*)&tmp = htonl(mappedAddr.addr);
		sprintf(data, "this is from %s %d %d", inet_ntoa(tmp), mappedAddr.port, i);
		
		unsigned long ip = ntohl(inet_addr(dstIp.c_str()));
		sendMessage(fd, data, strlen(data), ip, dstPort, false);
		*(int*)&tmp = htonl(ip);
		printf("send to %s %d\n", inet_ntoa(tmp), dstPort);

		unsigned int recvip;
		unsigned short recvport;
		int recvlen = sizeof(data);
		memset(data, 0, sizeof(data));
 		if (getMessage(fd, data, &recvlen, &recvip, &recvport, false))
 		{
 			*(int*)&tmp = htonl(recvip);
 			printf("recv %s from %s %d\n", data, inet_ntoa(tmp), recvport);
		}

		i++;
	}

	closesocket(fd);

	{
		char c;
		std::cin>>c;
	}
	return 0;
}

