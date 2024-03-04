FROM gomicro/goose

ADD *.sql /migrations/
ADD build/entrypoint.sh /migrations/
RUN chmod +x /migrations/entrypoint.sh

ENTRYPOINT ["/migrations/entrypoint.sh"]