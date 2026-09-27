#include "libhole.h"
#include "libchat.h"
#ifdef WIN32
#include <conio.h>
#endif

using namespace std;

enum echatcmd
{
	ecc_none,
	ecc_new,
	ecc_info,
	ecc_add,
	ecc_online,
};
echatcmd g_cmd = ecc_none;
std::string g_cmd_param1 = "";
std::string g_cmd_param2 = "";

#ifndef WIN32
int kbhit()
{
	fd_set rfds;
	struct timeval tv;
	int retval;

	/* Watch stdin (fd 0) to see when it has input. */
	FD_ZERO(&rfds);
	FD_SET(0, &rfds);
	/* Wait up to five seconds. */
	tv.tv_sec = 0;
	tv.tv_usec = 0;

	retval = select(1, &rfds, NULL, NULL, &tv);
	/* Don't rely on the value of tv now! */

	if (retval == -1) 
	{
		return 0;
	} 
	else if (retval)
	{
		return 1;
	}
	/* FD_ISSET(0, &rfds) will be true. */
	else
	{
		return 0;
	}
}
#endif

int parsearg(int argc, char* argv[])
{
	if (argc < 2)
	{
		return -1;
	}

	std::string cmd = argv[1];
	if (!strcmp(cmd.c_str(), "new"))
	{
		g_cmd = ecc_new;
		goto PARAM2;
	}
	else if (!strcmp(cmd.c_str(), "info"))
	{
		g_cmd = ecc_info;
		goto END;
	}
	else if (!strcmp(cmd.c_str(), "add"))
	{
		g_cmd = ecc_add;
		goto PARAM1;
	}
	else if (!strcmp(cmd.c_str(), "online"))
	{
		g_cmd = ecc_online;
		goto END;
	}

PARAM2:
	if (argc < 4)
	{
		return -1;
	}
	g_cmd_param2 = argv[3];

PARAM1:
	if (argc < 3)
	{
		return -1;
	}
	g_cmd_param1 = argv[2];

END:

	return 0;
}

void useage()
{
	printf("\
Fakechat \n\n\
Arg : [cmd] [param] \n\
\n\
Cmd List\n\
	new [name] [pwd]\n\
		create new user by name and password\n\
	info\n\
		get my info, somebody can use it to add me friend\n\
	add [info]\n\
		add somebody to my friend by info\n\
	online\n\
		login chat with friend\n\
");
}

int new_guid()
{
	if (!g_CConfigLoader.GetConfig().m_STUser.m_stracc.empty())
	{
		printf("already have user %s, overwrite? y/n\n", g_CConfigLoader.GetConfig().m_STUser.m_stracc.c_str());
		if (getchar() != 'y')
		{
			return 0;
		}
	}
	lc_newuser(g_cmd_param1, g_cmd_param2);
	return 0;
}

void info()
{
	if (g_CConfigLoader.GetConfig().m_STUser.m_stracc.empty())
	{
		printf("no info, plese create first, use [new] command\n");
	}
	else
	{
		char buff[100];
		sprintf(buff, "%s %s %d", g_CConfigLoader.GetConfig().m_STUser.m_stracc.c_str(), 
			g_CConfigLoader.GetConfig().m_STUser.m_strip.c_str(), 
			g_CConfigLoader.GetConfig().m_STUser.m_iport);
		std::string info = lc_des("fakechat", buff);
		printf("my info:\n%s\n", info.c_str());
	}
}

void add()
{
	if (g_CConfigLoader.GetConfig().m_STUser.m_stracc.empty())
	{
		printf("plese create first, use [new] command\n");
		return;
	}
	std::string info = lc_undes("fakechat", g_cmd_param1);
	std::vector<std::string> param = lc_token(info, " ");
	if (param.size() < 3)
	{
		printf("info error\n");
	}
	bool b1 = false;
	bool b2 = false;
	std::string key = lc_make_friend_key(param[0]);
	while (!b1 || !b2)
	{
		if (!b1)
		{
			b1 = lc_rpc_add(param[1], atoi(param[2].c_str()), param[0], key);
		}
		else
		{
			lc_process();
		}
		b2 = lc_is_friend(param[0]);
		printf("try again...\n");
	}
	lc_set_friend_skey(param[0], key);
	printf("add %s ok\n", lc_get_friend(param[0]).m_strname.c_str());
}

void on_recv_chat(const char * acc, const char * words)
{
	CConfigLoader::STConfig::STFriendList::STFriend f = lc_get_friend(acc);
	printf("[%s]: %s\n", f.m_strname.c_str(), words);
}

void online()
{
	if (g_CConfigLoader.GetConfig().m_STUser.m_stracc.empty())
	{
		printf("plese create first, use [new] command\n");
		return;
	}

	printf("input q to quit\n");
	printf("input [name] [words] to chat, eg. alice how are you\n");

	lc_set_chat_cb(on_recv_chat);

	while (1)
	{
		// input
		if (kbhit())
		{
			char buff[100] = {0};
			fgets(buff, sizeof(buff), stdin);
			std::string cmd = buff;
			if (cmd == "q")
			{
				printf("quit? y/n\n");
				if (getchar() == 'y')
				{
					break;
				}
				continue;
			}
			
			int pos = cmd.find(" ");
			if (pos == -1)
			{
				printf("input [name] [words] to chat, eg. alice how are you\n");
				continue;
			}

			std::string name = cmd.substr(0, pos);
			std::string words = cmd.substr(pos + 1);
			CConfigLoader::STConfig::STFriendList::STFriend f = lc_get_friend_by_name(name);
			if (f.m_stracc.empty())
			{
				printf("there is no friend named %s\n", name.c_str());
				continue;
			}

			if (!lc_rpc_chat(f.m_strip, f.m_iport, f.m_stracc, words))
			{
				printf("can not chat with friend %s\n", name.c_str());
				continue;
			}
		}
		else
		{
			lc_process();
		}
	}
}

int main(int argc, char* argv[])
{
	char buff[100];
	sprintf(buff, "%s %s %d", "598823FC359BBB43E556CEE9408AAA3C",
		"124.127.214.52",
		54394);
	std::string i = lc_des("fakechat", buff);
	std::string i2 = lc_undes("fakechat", i);

	if (!lc_ini())
	{
		goto EIXT;
	}

	if (parsearg(argc, argv) != 0)
	{
		useage();
		goto EIXT;
	}

	switch (g_cmd)
	{
	case ecc_none:
		useage();
		break;
	case ecc_new:
		new_guid();
		break;
	case ecc_info:
		info();
		break;
	case ecc_add:
		add();
		break;
	case ecc_online:
		online();
		break;
	default:
		useage();
		break;
	}

EIXT:
	lc_fini();
	return 0;
}

