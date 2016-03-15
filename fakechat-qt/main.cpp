#include "mainwindow.h"
#include <QApplication>

QApplication * g_app;

int main(int argc, char *argv[])
{
    QApplication a(argc, argv);
    g_app = &a;

    MainWindow w;
    w.show();

    return a.exec();
}
