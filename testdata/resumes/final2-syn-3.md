Selene Marrick
Application and Cloud Security Engineer
Raleigh, NC

# Summary

Security engineer with seven years of experience spanning Python development, application assessment, and AWS security. Works with backend teams to translate threat models and review findings into practical remediation. Combines secure code review and cloud posture work with incident response and selected detection engineering, with limited experience in endpoint operations and no frontend development background.

# Skills

- Languages: Python, SQL, Bash; reading familiarity with Go
- Application Security: threat modeling, secure code review, OWASP concepts, dependency analysis
- Cloud Security: AWS IAM, Organizations, CloudTrail, GuardDuty, Config, KMS
- Testing and Delivery: Semgrep, Bandit, OWASP ZAP, pytest, GitHub Actions
- Detection and Response: log investigation, Sigma fundamentals, incident triage, evidence timelines
- Additional Familiarity: OPA/Rego, container image scanning, Azure identity concepts

# Experience

## Application and Cloud Security Engineer | Asterquay Benefits Systems | Feb 2023 – Present

- Support application and AWS security for a benefits administration software company, partnering with eight backend teams on design reviews and remediation planning.
- Created lightweight threat-model templates for new data flows, covering trust boundaries, administrative access, and sensitive export handling; facilitated reviews for eleven service launches.
- Reviewed Python services for authorization, injection, and unsafe deserialization issues, working with developers to produce minimal reproductions and regression checks.
- Tuned Semgrep and dependency scanning workflows, reducing the weekly triage queue from approximately 140 findings to 55 while documenting suppressed rules and accepted risks.
- Analyzed IAM roles across eighteen AWS accounts and helped service owners remove unused permissions; tracked deployment failures during staged policy changes.
- Built CloudTrail-based detections for unusual role assumption and changes to audit logging, then tested them with the infrastructure team in controlled exercises.
- Served as security incident coordinator for a compromised integration credential, preserving an event timeline and coordinating revocation, access review, and follow-up improvements.

## Application Security Consultant (Contract) | Juniperwake Assurance | May 2022 – Jan 2023

- Completed a nine-month contract assessing Python APIs and AWS configurations for three clients in logistics and membership services.
- Combined manual code review with SAST and authenticated DAST, validating exploitability before recording severity and recommended fixes.
- Identified an object-level authorization weakness in a document API and worked with the client's developers to verify a fix across four affected endpoints.
- Reviewed IAM trust policies, public storage exposure, and encryption settings; prepared prioritized findings with operational context rather than raw scanner exports.
- Presented technical findings to engineering leads and maintained assessment notes, retest evidence, and scope boundaries for each engagement.

## Security Engineer | Morrowfen Records Software | Oct 2020 – Apr 2022

- Moved internally from backend development into a dedicated security role supporting a document retention product and its AWS environment.
- Introduced Bandit and dependency checks into six Python repositories, setting initial advisory thresholds and tracking recurring findings with maintainers.
- Conducted threat-model sessions for administrative workflows and reviewed changes involving signed downloads, service credentials, and tenant isolation.
- Assisted incident investigations by querying application logs and CloudTrail, documenting evidence gaps and proposing additional audit events.
- Wrote internal guidance for secrets handling and secure Python patterns, and answered implementation questions during engineering office hours.

## Python Software Developer | Morrowfen Records Software | Jul 2019 – Sep 2020

- Developed Python ingestion workers and REST endpoints for retention schedules, document metadata, and scheduled export requests.
- Improved a PostgreSQL-backed batch process by replacing repeated lookups with grouped queries, reducing a representative import from 28 to 12 minutes.
- Added pytest coverage for malformed documents and retry behavior, and helped investigate failed jobs during business-hours support.
- Worked with senior engineers on access checks and audit logging, which led to a growing focus on application security.

# Education

## Bachelor of Science, Computer Science | Bellshoal University | 2015 – 2019

Coursework included software engineering, databases, computer networks, and introductory cryptography.
