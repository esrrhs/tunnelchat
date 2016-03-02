#include "libchat.h"

using namespace std;

enum echatcmd
{
	ecc_none,
	ecc_new,
};
echatcmd g_cmd = ecc_none;
std::string g_cmd_param = "";

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
		goto PARAM;
	}

PARAM:
	if (argc < 3)
	{
		return -1;
	}
	g_cmd_param = argv[2];

	return 0;
}

void useage()
{
	printf("\
Fakechat \n\n\
Arg : [cmd] [param] \n\
\n\
Cmd List\n\
	new [name]\n\
		create new user by name\n\
");
}

int new_guid()
{
	std::string guid = lc_newguid(g_cmd_param);
	printf("%s\n", guid.c_str());
	return 0;
}

int main(int argc, char* argv[])
{
	lc_ini();

	if (parsearg(argc, argv) != 0)
	{
		useage();
		return 0;
	}

	switch (g_cmd)
	{
	case ecc_none:
		useage();
		break;
	case ecc_new:
		new_guid();
		break;
	default:
		useage();
		break;
	}

	return 0;
}

