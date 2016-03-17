#-------------------------------------------------
#
# Project created by QtCreator 2016-03-14T14:22:22
#
#-------------------------------------------------

QT       += core gui

greaterThan(QT_MAJOR_VERSION, 4): QT += widgets

TARGET = fakechat-qt
TEMPLATE = app


SOURCES += main.cpp\
        mainwindow.cpp

HEADERS  += mainwindow.h

FORMS    += mainwindow.ui

RESOURCES += \
    Images/appres.qrc

DISTFILES += \
    fakechat-qt.rc
	
RC_FILE = \
  fakechat-qt.rc
  