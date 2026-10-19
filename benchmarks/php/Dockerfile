FROM php:8.5-fpm
COPY pool.conf /usr/local/etc/php-fpm.d/zz-bench.conf
COPY php.ini /usr/local/etc/php/conf.d/zz-bench.ini
COPY *.php /srv/bench/
EXPOSE 9000
