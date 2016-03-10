#include "libchat.h"

using namespace std;

enum echatcmd
{
	ecc_none,
	ecc_new,
	ecc_info,
	ecc_add,
};
echatcmd g_cmd = ecc_none;
std::string g_cmd_param1 = "";
std::string g_cmd_param2 = "";

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
");
}

int new_guid()
{
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
	bool b = false;
	while (!b)
	{
		b = lc_send_add(param[1], atoi(param[2].c_str()), param[0]);
		b &= lc_is_friend(param[0]);
		printf("try again...\n");
	}
	printf("add %s ok\n", lc_get_friend(param[0]).m_strname.c_str());
}

int main(int argc, char* argv[])
{
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
	default:
		useage();
		break;
	}

EIXT:
	lc_fini();
	return 0;
}

