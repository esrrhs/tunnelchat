#include "mainwindow.h"
#include "ui_mainwindow.h"
#include "../libhole/libhole.h"
#include "../libchat/libchat.h"
#include <QMessageBox>
#include <QClipboard>

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

    if (g_CConfigLoader.GetConfig().m_STUser.m_stracc.empty())
    {
        ui->friendlistView->hide();
        ui->chattextBrowser->hide();
        ui->lineEdit->hide();
    }
    else
    {
        ui->pwd->hide();
        ui->pwdlineEdit->hide();
        ui->name->hide();
        ui->namelineEdit->hide();
        ui->createpushButton->hide();

        ui->chattextBrowser->hide();
        ui->lineEdit->hide();

        load_friend();
    }
}

MainWindow::~MainWindow()
{
    delete ui;
    lc_fini();
}

void MainWindow::on_actionInfo_triggered()
{
    char buff[100];
    sprintf(buff, "%s %s %d", g_CConfigLoader.GetConfig().m_STUser.m_stracc.c_str(),
        g_CConfigLoader.GetConfig().m_STUser.m_strip.c_str(),
        g_CConfigLoader.GetConfig().m_STUser.m_iport);
    std::string info = lc_des("fakechat", buff);

    QClipboard *board = QApplication::clipboard();
    board->setText(info.c_str());

    QMessageBox::information(this, "info", "your info has copied to Clipboard!");
}

void MainWindow::on_createpushButton_clicked()
{
    QString name = ui->namelineEdit->text();
    QString pwd = ui->pwdlineEdit->text();
    if (name.isEmpty() || pwd.isEmpty())
    {
        QMessageBox::information(this, "error", "need input name and password!");
        return;
    }

    if (name.indexOf(" ") != -1 || pwd.indexOf(" ") != -1)
    {
        QMessageBox::information(this, "error", "name and password should not have space!");
        return;
    }

    lc_newuser(name.toStdString(), pwd.toStdString());

    ui->pwd->hide();
    ui->pwdlineEdit->hide();
    ui->name->hide();
    ui->namelineEdit->hide();
    ui->createpushButton->hide();

    ui->friendlistView->show();
    ui->chattextBrowser->hide();
    ui->lineEdit->hide();

    load_friend();
}

void MainWindow::load_friend()
{
    ui->friendlistView->clear();

    for (int i = 0; i < g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend.size(); i++)
    {
        CConfigLoader::STConfig::STFriendList::STFriend f = g_CConfigLoader.GetConfig().m_STFriendList.m_vecSTFriend[i];

        int newmsg = 0;
        ChatMsgVec cmv = cmm[f.m_stracc];
        for (int j = 0; j < cmv.size(); j++)
        {
            if (cmv[j].isnew)
            {
                newmsg++;
            }
        }

        {
            QListWidgetItem * tmp = new QListWidgetItem(ui->friendlistView);
            ui->friendlistView->addItem(tmp);

            QWidget * l = new QWidget();
            QHBoxLayout * ll = new QHBoxLayout(l);

            QLabel * a = new QLabel(l);
            a->setText(f.m_strname.c_str());
            ll->addWidget(a);

            if (newmsg > 0)
            {
                QLabel * c = new QLabel(l);
                QString cstr = "(" + QString::number(newmsg, 10) + ")";
                c->setText(cstr);
                ll->addWidget(c);
            }

            l->show();

            ui->friendlistView->setItemWidget(tmp, l);

            tmp->setSizeHint(QSize(l->rect().width(), l->rect().height()));
        }
    }
}

void MainWindow::on_actionAdd_triggered()
{
    QClipboard *board = QApplication::clipboard();
    QString str = board->text();
    if (str.isEmpty())
    {
        QMessageBox::information(this, "error", "please copy info to Clipboard and try again!");
        return;
    }
    std::string info = lc_undes("fakechat", str.toStdString());
    std::vector<std::string> param = lc_token(info, " ");
    if (param.size() < 3)
    {
        QMessageBox::information(this, "error", "please copy info to Clipboard and try again!");
        return;
    }

    if (param[0] == g_CConfigLoader.GetConfig().m_STUser.m_stracc)
    {
        QMessageBox::information(this, "error", "can not add myself!");
        return;
    }

    bool b = false;
    std::string key = lc_make_friend_key(param[0]);
    while (!b)
    {
        b = lc_rpc_add(param[1], atoi(param[2].c_str()), param[0], key);
        b &= lc_is_friend(param[0]);
        ::Sleep(10);
    }
    lc_set_friend_skey(param[0], key);
    QString msg = "add ";
    msg += lc_get_friend(param[0]).m_strname.c_str();
    msg += " ok";
    QMessageBox::information(this, "info", msg);

    load_friend();
}
