#pragma once

#include "libhole.h"
#include <stdint.h>

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
