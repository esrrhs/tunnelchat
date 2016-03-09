#pragma once

#include "libhole.h"
#include <stdint.h>
#include "tinyxml.h"
#include <vector>
#include <string>
#include <deque> 
#include <algorithm> 

#ifdef _DEBUG
#define LCLOG(...) lclog("[DEBUG] ", __FILE__, __FUNCTION__, __LINE__, __VA_ARGS__);
#define LCERR(...) lclog("[ERROR] ", __FILE__, __FUNCTION__, __LINE__, __VA_ARGS__);
#define ISLOG 1
#else
#define LCLOG(...)
#define LCERR(...)
#define ISLOG 0
#endif

#define LC_MAX_MSG_LEN 512
#define LC_DES_BUFF_LEN 8

void lclog(const char * header, const char * file, const char * func, int pos, const char *fmt, ...);

#include "configloader.h"

// 初始化
bool lc_ini();

// 退出
bool lc_fini();

// 获取网关地址
std::string lc_get_mac();

// 生成guid
std::string lc_newguid(std::string param);

// MD5
std::string lc_md5(const char * str, size_t len);

// 获取毫秒
uint32_t lc_getmstick();

// 创建用户
void lc_newuser(std::string name, std::string pwd);

// 检查P2P，取得外网地址
bool lc_chekcp2p();

// StunAddress4转换
std::string lc_get_stunaddr_ip(StunAddress4 addr);

// 随机端口
int lc_randport();

// 16进制转换
int lc_atoi16(const std::string & str);
std::string lc_itoa16(uint32_t number);

// des
std::string lc_des(const std::string & strkey, const std::string & s_text);
std::string lc_undes(const std::string & strkey, const std::string & s_text);

// sleep
void lc_sleep(int32_t millionseconds);

// token
std::vector<std::string> lc_token(const std::string & str, const std::string & token);

// combine
std::string lc_combine(const std::vector<std::string> & strvec, const std::string & token);

// 发送消息
std::string lc_send(const std::string & ip, int port, const std::string & msg);

// 回复消息
void lc_resp(const std::string & ip, int port, const std::string & msgid, const std::string & msg);

// 发送udp消息
void lc_send_udp(const std::string & ip, int port, const std::string & msg);

// 接收消息
bool lc_recv(const std::string & msgid, std::string & ret);

// 处理消息
std::string lc_rpc(const std::string & ip, int port, const std::string & cmd, const std::string & msg);

// 处理消息
void lc_process();

// 消息回调
void lc_msg_process(const std::string & ip, int port, const std::string & msg);

// 获取好友
CConfigLoader::STConfig::STFriendList::STFriend lc_get_friend(const std::string & acc);
void lc_set_friend(const CConfigLoader::STConfig::STFriendList::STFriend & f);
bool lc_is_friend(const std::string & acc);

// 发送请求好友
bool lc_send_add(const std::string & ip, int port, const std::string & acc);
void lc_recv_add(const std::string & ip, int port, const std::string & msgid, const std::string & msg);
