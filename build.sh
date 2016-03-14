#! /bin/sh

BUILD_FLAG=""

if [ $# == 1 ] && [ $1 == "release" ];then
    BUILD_FLAG=" -DREMOD=ON"
fi

#libhole
cd libhole
rm CMakeCache.txt -rf
rm CMakeFiles -rf
rm cmake_install.cmake -rf
rm Makefile -rf
cmake . $BUILD_FLAG
make clean
make -j5
if [ $? -ne 0 ];then
	echo "build libhole fail"
	exit 1
fi
cd ..

#libchat
cd libchat
rm CMakeCache.txt -rf
rm CMakeFiles -rf
rm cmake_install.cmake -rf
rm Makefile -rf
cmake . $BUILD_FLAG
make clean
make -j5
if [ $? -ne 0 ];then
	echo "build libchat fail"
	exit 1
fi
cd ..

#test
cd test
rm CMakeCache.txt -rf
rm CMakeFiles -rf
rm cmake_install.cmake -rf
rm Makefile -rf
cmake . $BUILD_FLAG
make clean
make -j5
if [ $? -ne 0 ];then
	echo "build test fail"
	exit 1
fi
cd ..

#fakechat
cd fakechat
rm CMakeCache.txt -rf
rm CMakeFiles -rf
rm cmake_install.cmake -rf
rm Makefile -rf
cmake . $BUILD_FLAG
make clean
make -j5
if [ $? -ne 0 ];then
	echo "build fakechat fail"
	exit 1
fi
cd ..

echo "build ok"
