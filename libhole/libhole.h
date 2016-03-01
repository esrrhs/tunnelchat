#pragma once

#include <cassert>
#include <cstring>
#include <iostream>
#include <cstdlib>   
#include <stdio.h>
#include <stdarg.h>

#ifdef WIN32
#include <time.h>
#else
#include <sys/time.h>
#include <sys/types.h>
#include <unistd.h>
#include <netinet/in.h>
#include <errno.h>
#include <sys/socket.h>
#include <arpa/inet.h>
#endif

#include "udp.h"
#include "stun.h"
