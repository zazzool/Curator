-- Предполётный опрос базы.
--
-- Спрашивается ровно то, по чему видно, та ли это база и в каком она
-- состоянии. Ничего не меняется: выкатка, которая правит базу до того,
-- как человек сказал «да», однажды поправит не ту.
\pset footer off

SELECT current_database() AS база, current_user AS роль;

SELECT count(*) AS таблиц
  FROM information_schema.tables
 WHERE table_schema = 'public';

-- Пользователи студии. Ноль — это контур, в который нельзя войти, и
-- сказать об этом надо ДО выкатки, а не после: заводятся они утилитой из
-- того же образа (/app/user).
SELECT count(*) AS "пользователей студии" FROM users;

-- Источники и задачи: по ним видно, пустая это база или работающая.
SELECT count(*) AS источников FROM sources;
SELECT count(*) AS задач, count(*) FILTER (WHERE status = 'published') AS изданных
  FROM cases;
