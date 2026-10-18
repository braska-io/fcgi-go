FROM php:8.5-fpm
# intl, gd and opcache are not in the base image; the app cases use them.
RUN apt-get update \
 && apt-get install -y --no-install-recommends libicu-dev libpng-dev libjpeg-dev libfreetype6-dev \
 && docker-php-ext-configure gd --with-jpeg --with-freetype \
 && docker-php-ext-install -j"$(nproc)" intl gd opcache \
 && rm -rf /var/lib/apt/lists/*
COPY pool.conf /usr/local/etc/php-fpm.d/zz-compliance.conf
COPY php.ini /usr/local/etc/php/conf.d/zz-compliance.ini
COPY *.php /srv/fixtures/php/
COPY app/ /srv/fixtures/php/app/
RUN mkdir -p /state /state/sessions && chown -R www-data:www-data /state
EXPOSE 9000
