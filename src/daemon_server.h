#ifndef DAEMON_SERVER_H
#define DAEMON_SERVER_H

#define DAEMON_SERVER_H

#include <cstdlib>
#include <iostream>
#include <memory>
#include <utility>
#include <boost/asio.hpp>
#include <boost/asio/ssl.hpp>

#include <fstream>

#include <binn.h>

#include "config.h"

#define MSG_END_SYMBOLS_NUM 4

typedef boost::asio::ssl::stream<boost::asio::ip::tcp::socket> ssl_socket;

class Connection : public std::enable_shared_from_this<Connection> {
public:
    template <typename... Args>
    Connection(Args &&... args) noexcept : socket(new ssl_socket(std::forward<Args>(args)...)) {}
    std::unique_ptr<ssl_socket> socket;
};


// ---------------------------------------------------------------------

class DaemonServerSess : public std::enable_shared_from_this<DaemonServerSess> {
public:
    static constexpr ushort DAEMON_SERVER_MODE_NOAUTH     = 0;
    static constexpr ushort DAEMON_SERVER_MODE_AUTH       = 1;
    static constexpr ushort DAEMON_SERVER_MODE_CMD        = 2;
    static constexpr ushort DAEMON_SERVER_MODE_FILES      = 3;
    static constexpr ushort DAEMON_SERVER_MODE_STATUS     = 4;

    DaemonServerSess(std::shared_ptr<Connection> connection)
            : connection_(std::move(connection))
    {
        m_write_binn = binn_list();
    };

    ~DaemonServerSess() {
        binn_free(m_write_binn);
    };

    void start();
    ssl_socket::lowest_layer_type& socket();
    void handle_handshake(const boost::system::error_code& error);

private:
    static int append_end_symbols(char * buf, size_t length);
    void do_write();
    void do_read();
    size_t read_complete(size_t length);

    enum { max_length = 1024 };
    size_t read_length {0};
    char read_buf[max_length];

    binn *m_write_binn;
    std::shared_ptr<Connection> connection_;

    ushort mode {DAEMON_SERVER_MODE_NOAUTH};

};

// ---------------------------------------------------------------------

class DaemonServer
{
public:

    static constexpr ushort THREADS_NUM = 4;

DaemonServer(boost::asio::io_service& io_service, boost::asio::ip::tcp::endpoint endpoint)
        : acceptor_(io_service, endpoint),
          io_service_(io_service),
        context_(boost::asio::ssl::context::tlsv12)
{
        context_.set_options(
                boost::asio::ssl::context::default_workarounds
                | boost::asio::ssl::context::no_sslv2
                | boost::asio::ssl::context::single_dh_use);

        Config& config = Config::getInstance();

        if (config.private_key_password.length() > 0) {
            context_.set_password_callback(std::bind(&DaemonServer::get_password, this));
        }

        context_.use_certificate_chain_file(config.certificate_chain_file);
        context_.use_private_key_file(config.private_key_file, boost::asio::ssl::context::pem);
        context_.use_tmp_dh_file(config.dh_file);

        /**
         * verify client auth
         */
        context_.set_verify_mode(boost::asio::ssl::context::verify_fail_if_no_peer_cert | boost::asio::ssl::context::verify_peer);
        context_.load_verify_file(config.ca_certificate_file);

        start_accept();
};

private:
    void start_accept();
    void handle_accept(std::shared_ptr<DaemonServerSess> session, const boost::system::error_code& error);

    std::string get_password() const {
        Config& config = Config::getInstance();
        return config.private_key_password;
    }

    boost::asio::io_service& io_service_;
    boost::asio::ip::tcp::acceptor acceptor_;
    boost::asio::ssl::context context_;
};

// ---------------------------------------------------------------------

int run_server(const std::string& ip, ushort port);

#endif
