Mira Halden
Backend Software Engineer
Madison, WI

# Summary

Backend engineer with a May 2025 computer science degree and fifteen months of professional experience supporting a regional equipment rental platform. Uses Go and PostgreSQL to build small services, investigate operational issues, and improve scheduled processing. Comfortable owning a bounded change from design discussion through rollout, with senior guidance on architecture and production infrastructure.

# Skills

- Languages: Go, Python, SQL, Bash; introductory Rust
- Backend: net/http, REST APIs, JSON, database migrations, background jobs
- Data: PostgreSQL, SQLite, Redis fundamentals
- Cloud and Tools: AWS S3, CloudWatch, Docker, GitHub Actions, Git, Linux
- Practices: unit testing, code review, basic load testing, technical documentation; gRPC coursework

# Experience

## Software Engineer I | Cairnwheel Equipment Systems | Jul 2025 – Present

- Develop Go services for QuarryDesk, an equipment reservation and maintenance application, at a 460-person software company serving independent rental operators.
- Reworked a nightly reconciliation job to process reservations in bounded batches; reduced typical execution time from 41 to 18 minutes across approximately 280,000 records.
- Added PostgreSQL constraints and transaction handling to a maintenance scheduling endpoint after duplicate work orders appeared during concurrent submissions; partnered with a senior engineer on migration planning.
- Implemented CSV exports to AWS S3 with expiring download access and clearer validation messages, reducing export-related support tickets from about twelve to five per month.
- Write unit tests and integration checks for assigned changes, participate in pull request reviews, and update service runbooks when behavior changes.
- Use CloudWatch logs and SQL queries to investigate customer issues during business hours; escalate infrastructure and availability concerns to the platform team.
- Help estimate sprint work and discuss edge cases with support staff, including equipment transfers, canceled reservations, and incomplete historical records.

## Software Engineering Intern | Fenmar Utility Software | Jun 2024 – Aug 2024

- Built a Go command-line importer for municipal meter inventory files, replacing a spreadsheet cleanup step used by three implementation specialists.
- Added field-level validation and a preview mode that reported malformed identifiers before database writes; tested against anonymized samples supplied by the implementation team.
- Wrote PostgreSQL queries for a migration verification report and documented assumptions about missing installation dates and retired devices.
- Containerized the importer for local use and contributed tests to the existing continuous integration workflow under a staff engineer's supervision.
- Joined weekly customer issue reviews and presented the finished workflow to the engineering team at the end of the internship.

## Student Programming Assistant (Part-Time) | Alderbend College Computing Lab | Sep 2023 – May 2024

- Worked eight hours weekly while enrolled, maintaining Python scripts that summarized workstation availability for the campus computing lab.
- Replaced manual weekly totals with a SQLite-backed report, saving the lab coordinator an estimated two hours each reporting cycle.
- Added basic input checks and logging to scripts maintained by rotating student assistants.
- Helped classmates reproduce introductory programming errors and maintained short setup instructions for lab machines.

# Education

## Bachelor of Science, Computer Science | Alderbend College | 2021 – 2025

Completed coursework in databases, distributed systems, operating systems, and software engineering. Senior team project used Go, PostgreSQL, and AWS S3 to track shared laboratory equipment; responsible for the API and schema migrations.
