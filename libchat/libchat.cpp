#include "libchat.h"
#include <string.h> 

CConfigLoader g_CConfigLoader;

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

void lc_ini()
{
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
		CConfigLoader::STConfig::STSTUN tmp;
		tmp.m_strip = "stun.schlund.de";
		g_CConfigLoader.GetConfig().m_vecSTSTUN.push_back(tmp);
		g_CConfigLoader.SaveCfg("fakechat.xml");
	}
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
	sprintf(tmp, "%u_%u_%u_%s_%s", (unsigned int)rand(), t, ms, mac.c_str(), param.c_str());
	
	return lc_md5(tmp, strlen(tmp));
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
