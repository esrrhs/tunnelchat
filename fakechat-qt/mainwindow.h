#ifndef MAINWINDOW_H
#define MAINWINDOW_H

#include <QMainWindow>
#include <QListWidgetItem>

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

struct SendChatMsg
{
    std::string acc;
    std::string words;
    std::string msgid;
};
typedef std::list<SendChatMsg> SendList;

class MainWindow : public QMainWindow
{
    Q_OBJECT

public:
    explicit MainWindow(QWidget *parent = 0);
    ~MainWindow();

    void on_recv_chat(QString acc, QString words);

private slots:

    void on_actionInfo_triggered();

    void on_createpushButton_clicked();

    void on_actionAdd_triggered();

    void on_friendlistView_itemClicked(QListWidgetItem *item);

    void on_lineEdit_returnPressed();

private:
    void load_friend();

    void timerEvent( QTimerEvent *event );

private:
    Ui::MainWindow *ui;
    ChatMsgMap cmm;
    AddList al;
    QString curtalk;
    SendList sl;
};

#endif // MAINWINDOW_H
