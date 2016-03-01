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
fakechat \n\n\
arg : [cmd] [param] \n\
\n\
new [name]\n\
	create new user by name\n\
");
}

int main(int argc, char* argv[])
{
	initNetwork();

	if (parsearg(argc, argv) != 0)
	{
		useage();
		return -1;
	}

	return 0;
}

