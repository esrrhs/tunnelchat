#include "mainwindow.h"
#include "ui_mainwindow.h"
#include "../libhole/libhole.h"
#include "../libchat/libchat.h"
#include <QMessageBox>

#ifdef _DEBUG
#pragma comment(lib, "../bin/libchat_d.lib")
#else
#pragma comment(lib, "../bin/libchat.lib")
#endif

MainWindow::MainWindow(QWidget *parent) :
    QMainWindow(parent),
    ui(new Ui::MainWindow)
{
    ui->setupUi(this);

    if (!lc_ini())
    {
        QMessageBox::information(this, "error", "your network can not support p2p!");
        exit(0);
    }
}

MainWindow::~MainWindow()
{
    delete ui;
    lc_fini();
}
