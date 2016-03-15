#ifndef MAINWINDOW_H
#define MAINWINDOW_H

#include <QMainWindow>

namespace Ui {
class MainWindow;
}

struct ChatMsg
{
    std::string words;
    bool isnew;
};

typedef std::vector<ChatMsg> ChatMsgVec;
typedef std::map<std::string, ChatMsgVec> ChatMsgMap;

typedef std::map<std::string, std::string> AddList;

class MainWindow : public QMainWindow
{
    Q_OBJECT

public:
    explicit MainWindow(QWidget *parent = 0);
    ~MainWindow();

private slots:

    void on_actionInfo_triggered();

    void on_createpushButton_clicked();

    void on_actionAdd_triggered();

private:
    void load_friend();

    void timerEvent( QTimerEvent *event );

private:
    Ui::MainWindow *ui;
    ChatMsgMap cmm;
    AddList al;
};

#endif // MAINWINDOW_H
