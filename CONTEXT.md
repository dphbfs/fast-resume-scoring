# fast-resume-tailoring

Turns a job description into a prioritized list of what the employer asks for. It will later
feed a Resume Checker inside a larger resume-tailoring backend.

## Language

### Extraction

**Job Description**:
The plain-text posting a run analyzes; one Job Description per run.
_Avoid_: JD (in code/docs), posting, vacancy

**Job Summary**:
A short generated description of the role, used as background when judging Candidates.
_Avoid_: JD summary, abstract, overview

**Context Sentence**:
A sentence from the Job Description, stored once under a Ref and pointed to by the Requirements found in it.
_Avoid_: Snippet, excerpt, source text

**Ref**:
The stable identifier of a Context Sentence within one extraction result.
_Avoid_: Sentence ID, index

**Section**:
The role a Context Sentence plays in the Job Description: required, preferred, responsibilities, company, benefits, or other.
_Avoid_: Heading, block, part

**Candidate**:
A phrase produced by the sliding window over a Context Sentence that has not yet been judged.
_Avoid_: Window, n-gram, phrase, yield

**Requirement**:
One atomic thing the employer asks for (e.g. "Go", "Kubernetes", "5+ years backend"), linked to every Context Sentence it came from.
_Avoid_: Keyword, skill, tag, term

**Validation Round**:
The first Jev pass, which judges each Candidate against the Job Description and keeps those that are valid Requirements.
_Avoid_: Detection, classification pass

**Refinement Round**:
The second Jev pass, which sees all validated Requirements together, filters the list down, and assigns Importance.
_Avoid_: Ranking pass, dedup pass

**Filler**:
A validated phrase too generic to check against a resume (e.g. "team player", "fast-paced environment"); dropped in the Refinement Round.
_Avoid_: Fluff, noise, soft requirement

**Importance**:
How much the employer cares about a Requirement, as judged by Jev in the Refinement Round.
_Avoid_: Priority, weight, rank

### Components

**Requirement Extractor**:
The part that turns a Job Description into Requirements, Context Sentences, and Importance.
_Avoid_: Keyword extractor, parser, JD analyzer

**Resume Checker**:
The later part (not in V1) that takes Requirements and a resume and marks each Requirement present or missing.
_Avoid_: Matcher, scorer, classifier
