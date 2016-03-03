#pragma once

#include "libhole.h"
#include <stdint.h>
#include "tinyxml.h"
#include <vector>
#include <string>

#ifdef _DEBUG
#define LCLOG(...) lclog("[DEBUG] ", __FILE__, __FUNCTION__, __LINE__, __VA_ARGS__);
#define LCERR(...) lclog("[ERROR] ", __FILE__, __FUNCTION__, __LINE__, __VA_ARGS__);
#define ISLOG 1
#else
#define LCLOG(...)
#define LCERR(...)
#define ISLOG 0
#endif

void lclog(const char * header, const char * file, const char * func, int pos, const char *fmt, ...);

#include "configloader.h"

// 初始化
void lc_ini();

// 获取网关地址
std::string lc_get_mac();

// 生成guid
std::string lc_newguid(std::string param);

// MD5
std::string lc_md5(const char * str, size_t len);

// 获取毫秒
uint32_t lc_getmstick();
