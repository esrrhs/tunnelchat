#include "mainwindow.h"
#include <QApplication>

QApplication * g_app;
MainWindow * g_w;

int main(int argc, char *argv[])
{
    QApplication a(argc, argv);
    g_app = &a;

    MainWindow w;
    g_w = &w;
    w.show();

    return a.exec();
}
