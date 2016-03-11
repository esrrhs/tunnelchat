#include "libchat.h"
#include <string.h>

CConfigLoader g_CConfigLoader;
int g_fd = -1;
lc_on_recv_chat g_cb_on_recv_chat = 0;
int g_hb_count = 0;

struct MsgData
{
	std::string ip;
	int port;
	std::string msgid;
	std::string msg;
	std::string ret;
	bool isrecv;
	uint32_t sendtime;
	bool iscache;
};
std::vector<MsgData> g_MsgData;

void lclog(const char * header, const char * file, const char * func, int pos, const char *fmt, ...)
{
	FILE *pLog = NULL;
	time_t clock1;
	struct tm * tptr;
	va_list ap;

	pLog = fopen("fakechat.log", "a+");
	if (pLog == NULL)
	{
		return;
	}

	clock1 = time(0);
	tptr = localtime(&clock1);

	fprintf(pLog, "===========================[%d.%d.%d, %d.%d.%d]%s:%d,%s:===========================\n%s", 
		tptr->tm_year+1990,tptr->tm_mon+1,
		tptr->tm_mday,tptr->tm_hour,tptr->tm_min,
		tptr->tm_sec,file,pos,func,header);

	va_start(ap, fmt);
	vfprintf(pLog, fmt, ap);
	fprintf(pLog, "\n\n");
	va_end(ap);

	fclose(pLog);
}

bool lc_ini()
{
	LCLOG("lc_ini");

	srand(time(0));

	{
#ifdef WIN32 
		WORD wVersionRequested = MAKEWORD( 2, 2 );
		WSADATA wsaData;
		int err;

		err = WSAStartup( wVersionRequested, &wsaData );
		if ( err != 0 ) 
		{
			// could not find a usable WinSock DLL
			std::cerr << "Could not load winsock" << std::endl;
			assert(0); // is this is failing, try a different version that 2.2, 1.0 or later will likely work 
			exit(1);
		}

		/* Confirm that the WinSock DLL supports 2.2.*/
		/* Note that if the DLL supports versions greater    */
		/* than 2.2 in addition to 2.2, it will still return */
		/* 2.2 in wVersion since that is the version we      */
		/* requested.                                        */

		if ( LOBYTE( wsaData.wVersion ) != 2 ||
			HIBYTE( wsaData.wVersion ) != 2 ) 
		{
			/* Tell the user that we could not find a usable */
			/* WinSock DLL.                                  */
			WSACleanup( );
			std::cerr << "Bad winsock verion" << std::endl;
			assert(0); // is this is failing, try a different version that 2.2, 1.0 or later will likely work 
			exit(1);
		}    
#endif
	}

	if (!g_CConfigLoader.LoadCfg("fakechat.xml"))
	{
		{
			CConfigLoader::STConfig::STSTUNServer::STSTUN tmp;
			tmp.m_strip = "stun.schlund.de";
			g_CConfigLoader.GetConfig().m_STSTUNServer.m_vecSTSTUN.push_back(tmp);
		}

		g_CConfigLoader.GetConfig().m_STUser.m_iport = lc_randport();
	}

	// 开始查看局域网
	if (!lc_chekcp2p())
	{
		printf("sorry, your network cant support p2p\n");
		return false;
	}

	return true;
}

bool lc_chekcp2p()
{
	LCLOG("start lc_chekcp2p");
	StunAddress4 stunServerAddr;
	NatType stype = StunTypeUnknown;
	int trytime = 0;
	for (int i = 0; i < (int)g_CConfigLoader.GetConfig().m_STSTUNServer.m_vecSTSTUN.size(); i++)
	{
		std::string stunServerHost = g_CConfigLoader.GetConfig().m_STSTUNServer.m_vecSTSTUN[i].m_strip;
		bool ret = stunParseServerName(stunServerHost.c_str(), stunServerAddr);
		if (!ret)
		{
			LCERR("stunParseServerName Fail %s", stunServerHost.c_str());
			continue;
		}

		LCERR("start get type");

		stype = stunNatType( stunServerAddr, false, 0, 0, 
			0, 0);

		if (stype == StunTypeOpen || 
			stype == StunTypeIndependentFilter || 
			stype == StunTypeDependentFilter || 
			stype == StunTypePortDependedFilter)
		{
			break;
		}
		else
		{
			LCERR("type %d", stype);
			if (trytime < 10)
			{
				// 换个
				i--;
				trytime++;
				lc_sleep(100);
				LCERR("change port tyy again");
			}
		}
	}

	if (stype == StunTypeOpen || 
		stype == StunTypeIndependentFilter || 
		stype == StunTypeDependentFilter || 
		stype == StunTypePortDependedFilter)
	{
		LCLOG("stunNatType %d", stype);
	}
	else
	{
		LCERR("stunNatType Not Support Fail %d", stype);
		return false;
	}

	StunAddress4 mappedAddr;
	while (1)
	{
		LCLOG("start stunOpenSocket");
		int fd = stunOpenSocket(stunServerAddr, &mappedAddr,
			0, 0,
			false);

		if (fd != -1)
		{
			LCLOG("stunOpenSocket ok");
			closesocket(fd);
			break;
		}
	}

	g_fd = openPort(g_CConfigLoader.GetConfig().m_STUser.m_iport, 0, false);
	if (g_fd == -1)
	{
		LCERR("openPort fail");
		return false;
	}

#ifdef WIN32
	int iMode = 1;//0 == block， 1 == non-block
	ioctlsocket(g_fd, FIONBIO, (u_long FAR*) &iMode);
#else
	fcntl(g_fd, F_SETFL, O_NONBLOCK);
#endif

	g_CConfigLoader.GetConfig().m_STUser.m_strip = lc_get_stunaddr_ip(mappedAddr);

	LCLOG("Open P2P Socket OK %s %d %d", g_CConfigLoader.GetConfig().m_STUser.m_strip.c_str(), 
		g_CConfigLoader.GetConfig().m_STUser.m_iport,
		g_fd);

	return true;
}

bool lc_fini()
{
	g_CConfigLoader.SaveCfg("fakechat.xml");
	if (g_fd != -1)
	{
		closesocket(g_fd);
	}
	return true;
}

#ifdef WIN32
#pragma comment(lib, "netapi32.lib") 
typedef struct _ASTAT_
{
	ADAPTER_STATUS adapt;
	NAME_BUFFER    NameBuff [30];
}ASTAT, * PASTAT;
std::string lc_get_mac()
{
	ASTAT Adapter;

	NCB Ncb;
	UCHAR uRetCode;
	char NetName[50];
	LANA_ENUM lenum;
	int i;

	memset(&Ncb, 0, sizeof(Ncb));
	Ncb.ncb_command = NCBENUM;
	Ncb.ncb_buffer = (UCHAR *)&lenum;
	Ncb.ncb_length = sizeof(lenum);
	uRetCode = Netbios( &Ncb );
	if (uRetCode)
	{
		return "";
	}

	if (lenum.length <= 0)
	{
		return "";
	}
	
	i = 0;

	memset( &Ncb, 0, sizeof(Ncb) );
	Ncb.ncb_command = NCBRESET;
	Ncb.ncb_lana_num = lenum.lana[i];

	uRetCode = Netbios( &Ncb );
	if (uRetCode)
	{
		return "";
	}

	memset( &Ncb, 0, sizeof (Ncb) );
	Ncb.ncb_command = NCBASTAT;
	Ncb.ncb_lana_num = lenum.lana[i];

	strcpy( (char*)Ncb.ncb_callname, "*               ");
	Ncb.ncb_buffer = (UCHAR *) &Adapter;
	Ncb.ncb_length = sizeof(Adapter);

	uRetCode = Netbios( &Ncb );
	if (uRetCode)
	{
		return "";
	}

	char tmp[100];
	sprintf(tmp, "%02x:%02x:%02x:%02x:%02x:%02x",
		Adapter.adapt.adapter_address[0],
		Adapter.adapt.adapter_address[1],
		Adapter.adapt.adapter_address[2],
		Adapter.adapt.adapter_address[3],
		Adapter.adapt.adapter_address[4],
		Adapter.adapt.adapter_address[5] );
	
	std::string mac = tmp;

	return mac;
}
#else
#include <sys/ioctl.h> 
#include <sys/socket.h> 
#include <netinet/in.h> 
#include <net/if.h> 
std::string lc_get_mac()
{
	struct ifreq ifreq; 
	int sock = 0; 

	if ((sock=socket(AF_INET,SOCK_STREAM,0)) <0) 
	{
		return ""; 
	} 

	strcpy(ifreq.ifr_name, "eth0"); 
	if (ioctl(sock,SIOCGIFHWADDR,&ifreq) <0) 
	{ 
		return "";  
	} 

	char tmp[100] = {0};
	sprintf(tmp, "%02x:%02x:%02x:%02x:%02x:%02x", 
		(unsigned char)ifreq.ifr_hwaddr.sa_data[0], 
		(unsigned char)ifreq.ifr_hwaddr.sa_data[1], 
		(unsigned char)ifreq.ifr_hwaddr.sa_data[2], 
		(unsigned char)ifreq.ifr_hwaddr.sa_data[3], 
		(unsigned char)ifreq.ifr_hwaddr.sa_data[4], 
		(unsigned char)ifreq.ifr_hwaddr.sa_data[5]); 

	std::string mac = tmp;

	return mac;
}
#endif

std::string lc_newguid(std::string param)
{
	std::string mac = lc_get_mac();
	if (mac.empty())
	{
		return "";
	}

	unsigned int t = time(0);
	unsigned int ms = lc_getmstick();

	char tmp[256] = {0};
	sprintf(tmp, "%u_%u_%u_%s", (unsigned int)rand(), t, ms, mac.c_str());
	
	std::string tmpstr = tmp;
	tmpstr += "_" + param;

	return lc_md5(tmpstr.c_str(), tmpstr.size());
}

uint32_t lc_getmstick()
{
#ifdef WIN32
	return ::GetTickCount();
#else
	struct timeval tv;
	if(::gettimeofday(&tv, 0) == 0)
	{
		uint64_t t = tv.tv_sec * 1000;
		t += tv.tv_usec / 1000;
		return t & 0xffffffff;
	}
	return 0;
#endif
}

void lc_newuser( std::string name, std::string pwd )
{
	g_CConfigLoader.GetConfig().m_STUser.m_stracc = lc_newguid(name);
	g_CConfigLoader.GetConfig().m_STUser.m_strpwd = lc_newguid(pwd);
	g_CConfigLoader.GetConfig().m_STUser.m_strname = name;
}

std::string lc_get_stunaddr_ip( StunAddress4 addr )
{
	in_addr tmp;
	*(int*)&tmp = htonl(addr.addr);
	return inet_ntoa(tmp);
}

int lc_randport()
{
	return 50000 + rand() % 10000;
}

int lc_atoi16( const std::string & str )
{
	int32_t ret = 0;   
	for (int32_t i = 0; i < (int32_t)str.size(); i++)   
	{		
		const uint8_t & c = str[i];	
		if (c >= '0' && c <= '9')	
		{		
			ret = ret * 16 + (c - '0');	
		}	
		else if (c >= 'A' && c <= 'F')		
		{		
			ret = ret * 16 + (c - 'A') + 10;	
		}  
		else if (c >= 'a' && c <= 'f')		
		{		
			ret = ret * 16 + (c - 'a') + 10;	
		}  
	}
	return ret;
}

std::string lc_itoa16( uint32_t number )
{
	std::string ret;

	// temporary buffer for 16 numbers
	char tmpbuf[16]={0};
	uint32_t idx = 15;

	// special case '0'

	if (!number)
	{
		tmpbuf[14] = '0';
		ret = &tmpbuf[14];
		return ret;
	}

	// add numbers
	const uint8_t chars[]="0123456789ABCDEF";
	while(number && idx)
	{
		--idx;
		tmpbuf[idx] = chars[(number % 16)];
		number /= 16;
	}

	ret = &tmpbuf[idx];
	return ret;
}

std::string lc_des_str( const std::string & strkey, const std::string & s_text );
std::string lc_des( const std::string & strkey, const std::string & s_text )
{
	std::string k = lc_md5(strkey.c_str(), strkey.size());
	std::string v;
	for (int i = 0; i < (int)s_text.size(); i += LC_DES_BUFF_LEN)
	{
		v += lc_des_str(k, s_text.c_str() + i);
	}
	return v;
}

std::string lc_undes_str( const std::string & strkey, const std::string & s_text );
std::string lc_undes( const std::string & strkey, const std::string & s_text )
{
	std::string k = lc_md5(strkey.c_str(), strkey.size());
	std::string v;
	for (int i = 0; i < (int)s_text.size(); i += LC_DES_BUFF_LEN * 2)
	{
		v += lc_undes_str(k, s_text.c_str() + i);
	}
	return v;
}

void lc_sleep( int32_t millionseconds )
{
#if defined(WIN32)
	Sleep(millionseconds);
#else
	usleep(millionseconds * 1000);
#endif
}

std::vector<std::string> lc_token( const std::string & str, const std::string & token )
{
	std::vector<std::string> ret;
	std::string tmp = str;
	char * find = strtok((char*)tmp.c_str(), token.c_str());
	while (find)
	{
		ret.push_back(find);
		find = strtok(0, token.c_str());
	}
	return ret;
}

std::string lc_combine( const std::vector<std::string> & strvec, const std::string & token )
{
	std::string ret;
	for (int i = 0; i < (int)strvec.size(); i++)
	{
		if (i != 0)
		{
			ret += token;
		}
		ret += strvec[i];
	}
	return ret;
}

std::string lc_send( const std::string & ip, int port, const std::string & msg )
{
	std::string msgid = lc_newguid(msg);

	std::string realmsg = msgid + " " + msg;
	lc_send_udp(ip, port, realmsg);

	MsgData tmp;
	tmp.ip = ip;
	tmp.port = port;
	tmp.msgid = msgid;
	tmp.msg = realmsg;
	tmp.isrecv = false;
	tmp.iscache = false;
	tmp.sendtime = time(0);

	g_MsgData.push_back(tmp);

	return msgid;
}

void lc_resp( const std::string & ip, int port, const std::string & msgid, const std::string & msg )
{
	std::string realmsg = msgid + " res " + msg;
	lc_send_udp(ip, port, realmsg);

	MsgData tmp;
	tmp.ip = ip;
	tmp.port = port;
	tmp.msgid = msgid;
	tmp.msg = realmsg;
	tmp.isrecv = false;
	tmp.iscache = true;
	tmp.sendtime = time(0);

	g_MsgData.push_back(tmp);
}

void lc_send_udp( const std::string & ip, int port, const std::string & msg )
{
	assert(msg.size() < LC_MAX_MSG_LEN);
	unsigned long uip = ntohl(inet_addr(ip.c_str()));
	sendMessage(g_fd, msg.c_str(), msg.size(), uip, port, false);
	LCLOG("send %s %d : %s", ip.c_str(), port, msg.c_str());
}

bool lc_recv( const std::string & msgid, std::string & ret )
{
	for (int i = 0; i < (int)g_MsgData.size(); i++)
	{
		if (g_MsgData[i].msgid == msgid)
		{
			if (g_MsgData[i].isrecv)
			{
				ret = g_MsgData[i].ret;
			}
			return true;
		}
	}
	return false;
}

std::string lc_rpc( const std::string & ip, int port, const std::string & cmd, const std::string & msg )
{
	std::string msgid = lc_send(ip, port, cmd + " " + msg);
	std::string ret;
	while (lc_recv(msgid, ret))
	{
		lc_process();
	}
	return ret;
}

struct remove_msgdata
{
	bool operator()(const MsgData & tmp)
	{
		uint32_t now = time(0);
		if (now - tmp.sendtime > LC_MAX_RPC_TIME)
		{
			return true;
		}
		return false;
	}
};

void lc_process()
{
	// 定时更新信息
	if (g_hb_count % LC_SYNC_TIME == 0)
	{
		for (int i = 0; i < (int)g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.size(); i++)
		{
			CConfigLoader::STConfig::STFriendList::STFriend & tmp = g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i];
			lc_send_sync(tmp.m_strip, tmp.m_iport, tmp.m_stracc);
		}
		g_hb_count++;
	}

	// 心跳
	if (g_hb_count % LC_HB_TIME == 0)
	{
		for (int i = 0; i < (int)g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.size(); i++)
		{
			CConfigLoader::STConfig::STFriendList::STFriend & tmp = g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i];
			lc_send_udp(tmp.m_strip, tmp.m_iport, "hb");
		}
		g_hb_count++;
	}

	// 重发
	if (g_hb_count % LC_RESEND_TIME == 0)
	{
		for (int i = 0; i < (int)g_MsgData.size(); i++)
		{
			MsgData & tmp = g_MsgData[i];
			if (!tmp.iscache)
			{
				lc_send_udp(tmp.ip, tmp.port, tmp.msg);
			}
		}
	}

	// 接收
	{
		char data[LC_MAX_MSG_LEN] = {0};
		unsigned int recvip;
		unsigned short recvport;
		int recvlen = sizeof(data);
		memset(data, 0, sizeof(data));
		if (getMessage(g_fd, data, &recvlen, &recvip, &recvport, false))
		{
			in_addr tmpaddr;
			*(int*)&tmpaddr = htonl(recvip);
			lc_msg_process(inet_ntoa(tmpaddr), recvport, data);
		}
	}

	// 超时处理
	g_MsgData.erase(std::remove_if(g_MsgData.begin(), g_MsgData.end(), remove_msgdata()), g_MsgData.end());

	g_hb_count++;
}

void lc_msg_process( const std::string & ip, int port, const std::string & msg )
{
	if (msg == "hb")
	{
		return;
	}
	std::vector<std::string> retvec = lc_token(msg, " ");
	// key + command + data
	if (retvec.size() < 3)
	{
		assert(0);
		return;
	}
	std::string msgid = retvec[0];
	std::string msgcmd = retvec[1];
	retvec.erase(retvec.begin());
	retvec.erase(retvec.begin());
	std::string msgdata = lc_combine(retvec, " ");
	LCLOG("recv %s %d : %s %s %s", ip.c_str(), port, msgid.c_str(), msgcmd.c_str(), msgdata.c_str());
	if (msgcmd == "res")
	{
		for (int i = 0; i < (int)g_MsgData.size(); i++)
		{
			if (g_MsgData[i].msgid == msgid && 
				g_MsgData[i].ip == ip && 
				g_MsgData[i].port == port)
			{
				g_MsgData[i].isrecv = true;
				g_MsgData[i].ret = msgdata;
			}
		}
	}
	else
	{
		// cache
		for (int i = 0; i < (int)g_MsgData.size(); i++)
		{
			if (g_MsgData[i].msgid == msgid && 
				g_MsgData[i].ip == ip && 
				g_MsgData[i].port == port)
			{
				lc_send_udp(ip, port, g_MsgData[i].msg);
				return;
			}
		}

		if (msgcmd == "add")
		{
			lc_on_rpc_add(ip, port, msgid, msgdata);
		}
		else if (msgcmd == "chat")
		{
			lc_on_rpc_chat(ip, port, msgid, msgdata);
		}
		else if (msgcmd == "sync")
		{
			lc_recv_sync(ip, port, msgdata);
		}
		else
		{
			lc_resp(ip, port, msgid, "ok");
		}
	}
}

CConfigLoader::STConfig::STFriendList::STFriend lc_get_friend( const std::string & acc )
{
	CConfigLoader::STConfig::STFriendList::STFriend tmp;
	for (int i = 0; i < (int)g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.size(); i++)
	{
		if (g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i].m_stracc == acc)
		{
			tmp = g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i];
			break;
		}
	}
	return tmp;
}

void lc_set_friend( const CConfigLoader::STConfig::STFriendList::STFriend & f )
{
	for (int i = 0; i < (int)g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.size(); i++)
	{
		if (g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i].m_stracc == f.m_stracc)
		{
			g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i] = f;
			return;
		}
	}
	g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.push_back(f);
}

bool lc_is_friend( const std::string & acc )
{
	return !lc_get_friend(acc).m_stracc.empty();
}

CConfigLoader::STConfig::STFriendList::STFriend lc_get_friend_by_name( const std::string & name )
{
	CConfigLoader::STConfig::STFriendList::STFriend tmp;
	for (int i = 0; i < (int)g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.size(); i++)
	{
		if (g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i].m_strname == name)
		{
			tmp = g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i];
			break;
		}
	}
	return tmp;
}

void lc_set_friend_skey( const std::string & acc, const std::string & key )
{
	for (int i = 0; i < (int)g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.size(); i++)
	{
		if (g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i].m_stracc == acc)
		{
			g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i].m_strskey = key;
			return;
		}
	}
}

std::string lc_make_friend_key( const std::string & acc )
{
	std::string tmp;
	tmp += g_CConfigLoader.GetConfig().m_STUser.m_strpwd;
	tmp += acc;
	std::string key = lc_md5(tmp.c_str(), tmp.size());
	return key;
}

bool lc_rpc_add( const std::string & ip, int port, const std::string & acc, const std::string & key )
{
	std::string msg = g_CConfigLoader.GetConfig().m_STUser.m_stracc;
	msg += " " + g_CConfigLoader.GetConfig().m_STUser.m_strname;
	msg += " " + key;
	msg += " " + acc;
	std::string emsg = lc_des("add", msg);
	std::string ret = lc_rpc(ip, port, "add", emsg);
	if (ret != "ok")
	{
		return false;
	}
	return true;
}

void lc_on_rpc_add( const std::string & ip, int port, const std::string & msgid, const std::string & msg )
{
	std::string smsg = lc_undes("add", msg);
	std::vector<std::string> msgvec = lc_token(smsg, " ");
	if (msgvec.size() < 4)
	{
		LCERR("error param %s", smsg.c_str());
		assert(0);
		return;
	}

	std::string acc = msgvec[0];
	std::string name = msgvec[1];
	std::string key = msgvec[2];
	std::string wantacc = msgvec[3];

	if (wantacc != g_CConfigLoader.GetConfig().m_STUser.m_stracc)
	{
		LCERR("diff acc %s %s", wantacc.c_str(), g_CConfigLoader.GetConfig().m_STUser.m_stracc.c_str());
		assert(0);
		return;
	}

	CConfigLoader::STConfig::STFriendList::STFriend f = lc_get_friend(acc);
	// 检测本地是否有一个acc了 key是否一致
	if (f.m_stracc == acc && f.m_strrkey != key)
	{
		LCERR("diff key %s %s %s", acc.c_str(), f.m_strrkey.c_str(), key.c_str());
		assert(0);
		return;
	}

	lc_resp(ip, port, msgid, "ok");

	f.m_iport = port;
	f.m_strip = ip;
	f.m_stracc = acc;
	f.m_strrkey = key;
	f.m_strname = name;
	lc_set_friend(f);

	LCLOG("lc_recv_add %s %d %s %s", ip.c_str(), port, acc.c_str(), name.c_str());
}

bool lc_rpc_chat( const std::string & ip, int port, const std::string & acc, const std::string & words )
{
	CConfigLoader::STConfig::STFriendList::STFriend f = lc_get_friend(acc);
	if (f.m_stracc.empty())
	{
		LCERR("chat no friend %s %s", acc.c_str(), words.c_str());
		assert(0);
		return false;
	}

	std::string key = f.m_strskey;
	std::string ewords = lc_des(key, words);

	std::string msg = g_CConfigLoader.GetConfig().m_STUser.m_stracc;
	msg += " " + ewords;
	std::string emsg = lc_des("chat", msg);

	std::string ret = lc_rpc(ip, port, "chat", emsg);
	if (ret != "ok")
	{
		return false;
	}
	return true;
}

void lc_on_rpc_chat( const std::string & ip, int port, const std::string & msgid, const std::string & msg )
{
	std::string smsg = lc_undes("chat", msg);
	std::vector<std::string> msgvec = lc_token(smsg, " ");
	if (msgvec.size() < 2)
	{
		LCERR("error param %s", smsg.c_str());
		assert(0);
		return;
	}

	std::string acc = msgvec[0];
	std::string words = msgvec[1];
	CConfigLoader::STConfig::STFriendList::STFriend f = lc_get_friend(acc);
	if (f.m_stracc.empty())
	{
		LCERR("chat no friend %s %s", acc.c_str(), words.c_str());
		assert(0);
		return;
	}

	if (!g_cb_on_recv_chat)
	{
		LCERR("no chat callback %s %s", acc.c_str(), words.c_str());
		assert(0);
		return;
	}

	std::string swords = lc_undes(f.m_strrkey, words);

	lc_resp(ip, port, msgid, "ok");

	g_cb_on_recv_chat(acc.c_str(), swords.c_str());

	LCLOG("chat from %s %s", acc.c_str(), swords.c_str());
}

void lc_set_chat_cb( lc_on_recv_chat cb )
{
	g_cb_on_recv_chat = cb;
}

void lc_send_sync( const std::string & ip, int port, const std::string & acc )
{
	if (g_CConfigLoader.GetConfig().m_STUser.m_stracc.empty())
	{
		return;
	}

	CConfigLoader::STConfig::STFriendList::STFriend f = lc_get_friend(acc);
	if (f.m_stracc.empty())
	{
		LCERR("no friend %s", acc.c_str());
		assert(0);
		return;
	}

	char buff[100];
	sprintf(buff, "%s %d %s", g_CConfigLoader.GetConfig().m_STUser.m_strip.c_str(), 
		g_CConfigLoader.GetConfig().m_STUser.m_iport,
		g_CConfigLoader.GetConfig().m_STUser.m_strname.c_str());

	std::string info = buff;

	std::string key = f.m_strskey;
	std::string einfo = lc_des(key, info);

	std::string msg = g_CConfigLoader.GetConfig().m_STUser.m_stracc;
	msg += " " + einfo;

	std::string emsg = lc_des("sync", msg);
	emsg = "0 sync " + emsg;

	lc_send_udp(ip, port, emsg);
}

void lc_recv_sync( const std::string & ip, int port, const std::string & msg )
{
	std::string smsg = lc_undes("sync", msg);
	std::vector<std::string> msgvec = lc_token(smsg, " ");
	if (msgvec.size() < 2)
	{
		LCERR("error param %s", smsg.c_str());
		assert(0);
		return;
	}

	std::string acc = msgvec[0];
	std::string info = msgvec[1];
	CConfigLoader::STConfig::STFriendList::STFriend f = lc_get_friend(acc);
	if (f.m_stracc.empty())
	{
		LCERR("no friend %s", acc.c_str());
		assert(0);
		return;
	}

	std::string sinfo = lc_undes(f.m_strrkey, info);
	std::vector<std::string> infovec = lc_token(sinfo, " ");
	if (infovec.size() < 3)
	{
		LCERR("error param %s", smsg.c_str());
		assert(0);
		return;
	}

	std::string sip = infovec[0];
	int sport = atoi(infovec[1].c_str());
	std::string sname = infovec[2];

	f.m_iport = sport;
	f.m_strip = sip;
	f.m_strname = sname;

	LCLOG("sync from %s %s %d", sname.c_str(), sip.c_str(), sport);
}
